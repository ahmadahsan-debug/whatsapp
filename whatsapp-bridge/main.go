package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/mdp/qrterminal"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
)

const apiPort = 8080

func main() {
	ctx := context.Background()
	logger := newSafeLogger("Client")
	dbLog := newSafeLogger("Database")
	logger.Infof("Starting WhatsApp bridge...")
	go pruneOldLogs(logDir, logKeepDays)

	if err := os.MkdirAll("store", 0o700); err != nil {
		logger.Errorf("Failed to create store directory: %v", err)
		os.Exit(1)
	}
	os.MkdirAll(configDir, 0o755)

	container, err := sqlstore.New(ctx, "sqlite3", "file:store/whatsapp.db?_foreign_keys=on", dbLog)
	if err != nil {
		logger.Errorf("Failed to connect to database: %v", err)
		os.Exit(1)
	}

	// The device store holds the login (session) information.
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			deviceStore = container.NewDevice()
			logger.Infof("Created new device")
		} else {
			logger.Errorf("Failed to get device: %v", err)
			os.Exit(1)
		}
	}

	client := whatsmeow.NewClient(deviceStore, logger)
	if client == nil {
		logger.Errorf("Failed to create WhatsApp client")
		os.Exit(1)
	}

	messageStore, err := NewMessageStore()
	if err != nil {
		logger.Errorf("Failed to initialize message store: %v", err)
		os.Exit(1)
	}
	defer messageStore.Close()

	sender := NewSender(client, messageStore)

	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			handleMessage(client, messageStore, v, logger)
		case *events.HistorySync:
			handleHistorySync(client, messageStore, v, logger)
		case *events.Connected:
			loggedOut.Store(false)
			logger.Infof("Connected to WhatsApp")
		case *events.Disconnected:
			logger.Warnf("Disconnected from WhatsApp (will retry automatically)")
		case *events.LoggedOut:
			loggedOut.Store(true)
			logger.Warnf("Device logged out. Stop the service and run the bridge in a terminal to scan a new QR code")
		}
	})

	// Start the API first, so /api/health answers even while connecting or
	// logged out.
	startRESTServer(client, messageStore, sender, apiPort)

	if client.Store.ID == nil {
		// Not linked yet. Pairing needs a QR code on a real terminal; a
		// background service has none, so it just reports logged_out.
		if !isatty.IsTerminal(os.Stdout.Fd()) {
			logger.Errorf("Not linked to WhatsApp and no terminal available. Run the bridge in a terminal to scan the QR code")
			waitForExit(client)
			return
		}
		qrChan, _ := client.GetQRChannel(ctx)
		if err := client.Connect(); err != nil {
			logger.Errorf("Failed to connect: %v", err)
			os.Exit(1)
		}
		paired := false
		timeout := time.After(3 * time.Minute)
	pairing:
		for {
			select {
			case evt, ok := <-qrChan:
				if !ok {
					break pairing
				}
				switch evt.Event {
				case "code":
					// The QR code goes to the terminal only, never to a log file.
					os.Stdout.WriteString("\nScan this QR code with your WhatsApp app\n(Settings > Linked devices > Link a device):\n")
					qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				case "success":
					paired = true
					break pairing
				}
			case <-timeout:
				break pairing
			}
		}
		if !paired {
			logger.Errorf("QR code was not scanned in time. Start the bridge again to get a new one")
			client.Disconnect()
			os.Exit(1)
		}
		logger.Infof("Paired successfully. Waiting for your message history to sync")
	} else if err := client.Connect(); err != nil {
		// Do not exit: the library retries, and health reports "connecting".
		logger.Errorf("Failed to connect: %v", err)
	}

	waitForExit(client)
}

func waitForExit(client *whatsmeow.Client) {
	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, syscall.SIGINT, syscall.SIGTERM)
	appLog("Bridge running. Press Ctrl+C to stop.")
	<-exitChan
	appLog("Disconnecting...")
	client.Disconnect()
}
