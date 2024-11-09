package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	command := os.Args[1]
	switch command {
	case "message":
		listenForMessages()
	case "qr":
		generateQR()
	case "help":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
	}
}

func printHelp() {
	fmt.Println("WhatsApp CLI Application")
	fmt.Println("\nUsage:")
	fmt.Println("  go run main.go <command>")
	fmt.Println("\nCommands:")
	fmt.Println("  message    Listen for incoming WhatsApp messages")
	fmt.Println("  qr        Generate QR code for new WhatsApp login")
	fmt.Println("  help      Show this help message")
}

func setupClient() (*whatsmeow.Client, error) {
	logger := waLog.Stdout("Main", "DEBUG", true)
	dbLog := waLog.Stdout("Database", "DEBUG", true)

	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %v", err)
	}

	dbPath := dir + "/whatsapp.db"
	fmt.Printf("Database path: %s\n", dbPath)

	dbParams := "?_foreign_keys=on" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" + // Use WAL mode for better concurrency
		"&_pragma=synchronous(NORMAL)" + // Slightly faster, still safe
		"&_pragma=busy_timeout(5000)" + // Wait up to 5 seconds when database is locked
		"&_pragma=cache_size(-2000)" // 2MB cache size

	container, err := sqlstore.New("sqlite", "file:"+dbPath+dbParams, dbLog)
	if err != nil {
		if strings.Contains(err.Error(), "foreign keys are not enabled") {
			fmt.Println("Database appears to be corrupted, removing and creating new one...")
			os.Remove(dbPath)
			container, err = sqlstore.New("sqlite", "file:"+dbPath+dbParams, dbLog)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to database: %v", err)
			}
		} else {
			return nil, fmt.Errorf("failed to connect to database: %v", err)
		}
	}

	deviceStore, _ := container.GetFirstDevice()
	// deviceStore := container.NewDevice()
	if deviceStore == nil {
		return nil, fmt.Errorf("failed to create device: device store is nil")
	}
	fmt.Println("Device store ...", deviceStore.ID)

	client := whatsmeow.NewClient(deviceStore, logger)

	if client.Store.ID == nil {
		fmt.Println("Debug: No device ID found in store")
	} else {
		fmt.Printf("Debug: Found device ID: %s\n", client.Store.ID.String())
	}

	return client, nil
}

func listenForMessages() {
	client, err := setupClient()
	if err != nil {
		fmt.Printf("Error setting up client: %v\n", err)
		return
	}

	// Add message handler
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			// Get message content
			var content string
			if v.Message.GetConversation() != "" {
				content = v.Message.GetConversation()
			} else if v.Message.GetExtendedTextMessage() != nil {
				content = v.Message.GetExtendedTextMessage().GetText()
			} else if img := v.Message.GetImageMessage(); img != nil {
				content = fmt.Sprintf("[Image] Caption: %s", img.GetCaption())
			} else if video := v.Message.GetVideoMessage(); video != nil {
				content = fmt.Sprintf("[Video] Caption: %s", video.GetCaption())
			} else if doc := v.Message.GetDocumentMessage(); doc != nil {
				content = fmt.Sprintf("[Document] Filename: %s", doc.GetFileName())
			} else if audio := v.Message.GetAudioMessage(); audio != nil {
				content = "[Audio]"
				if audio.GetPTT() {
					content = "[Voice Message]"
				}
			} else if sticker := v.Message.GetStickerMessage(); sticker != nil {
				content = "[Sticker]"
			} else if reaction := v.Message.GetReactionMessage(); reaction != nil {
				content = fmt.Sprintf("[Reaction] %s to message: %s", reaction.GetText(), reaction.GetKey().GetId())
			} else {
				content = "[Unknown Message Type]"
			}

			// Get sender info
			senderInfo := v.Info.PushName
			if senderInfo == "" {
				senderInfo = v.Info.Sender.String()
			}

			// Get chat info
			chatInfo := "Private Message"
			if v.Info.Chat.Server == "g.us" {
				chatInfo = "Group Message"
			}

			// Print message details
			fmt.Printf("\n=== New Message ===\n")
			fmt.Printf("From: %s\n", senderInfo)
			fmt.Printf("Type: %s\n", chatInfo)
			if v.Info.Chat.Server == "g.us" {
				fmt.Printf("Group: %s\n", v.Info.Chat.User)
			}
			fmt.Printf("Time: %s\n", v.Info.Timestamp.Local().Format("2006-01-02 15:04:05"))
			fmt.Printf("Content: %s\n", content)
			fmt.Println("=================")
		}
	})

	if client.Store.ID == nil {
		fmt.Println("No existing login found. Please run 'go run main.go qr' first to log in.")
		return
	}

	err = client.Connect()
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}

	fmt.Println("Connected successfully!")
	fmt.Println("Listening for messages... (Press Ctrl+C to exit)")

	// Handle interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}

func generateQR() {
	client, err := setupClient()
	if err != nil {
		fmt.Printf("Error setting up client: %v\n", err)
		return
	}

	// Add event handler to monitor connection status
	client.AddEventHandler(func(evt interface{}) {
		switch evt.(type) {
		case *events.Connected:
			fmt.Println("Connected to WhatsApp!")
		case *events.StreamReplaced:
			fmt.Println("Connection replaced by another login")
			os.Exit(1)
		case *events.LoggedOut:
			fmt.Println("Device logged out!")
			os.Exit(1)
		case *events.PushNameSetting:
			fmt.Printf("Push name changed")
		}
	})

	qrChan, _ := client.GetQRChannel(context.Background())
	err = client.Connect()
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}

	fmt.Println("Waiting for QR code...")
	loginSuccess := false

	for evt := range qrChan {
		if evt.Event == "code" {
			qr, err := qrcode.New(evt.Code, qrcode.Medium)
			if err != nil {
				fmt.Printf("Failed to generate QR code: %v\n", err)
				continue
			}

			art := qr.ToSmallString(false)
			art = strings.TrimSpace(art)

			fmt.Println("Scan this QR code in WhatsApp:")
			fmt.Println(art)
		} else if evt.Event == "success" {
			loginSuccess = true
			fmt.Println("QR code scanned successfully!")
			fmt.Println("Waiting for full login to complete...")

			// Wait for initial connection
			time.Sleep(15 * time.Second)

			if client.Store.ID == nil {
				fmt.Println("Error: Failed to get device ID after login")
				return
			}

			fmt.Printf("Successfully logged in as %s\n", client.Store.ID.String())

			// Force a store flush to ensure data is written to database
			err = client.Store.Save()
			if err != nil {
				fmt.Printf("Error saving to database: %v\n", err)
				return
			}

			fmt.Println("\nStarting initial sync...")
			fmt.Println("Please wait for the sync to complete (this may take a few minutes)")
			fmt.Println("You should see your WhatsApp contacts and chats appear on your phone")
			fmt.Println("Press Ctrl+C when the sync is complete")

			// Add handler for sync status
			client.AddEventHandler(func(evt interface{}) {
				switch v := evt.(type) {
				case *events.AppStateSyncComplete:
					fmt.Printf("Sync completed for %s\n", v.Name)
				}
			})
		} else {
			fmt.Println("Login event:", evt.Event)
		}
	}

	if !loginSuccess {
		fmt.Println("QR code scanning was not completed successfully")
		return
	}

	// Keep connection open and wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nDisconnecting safely...")

	// Force final save before disconnecting
	err = client.Store.Save()
	if err != nil {
		fmt.Printf("Error saving final state to database: %v\n", err)
	}

	client.Disconnect()

	// Verify the database
	verifyClient, err := setupClient()
	if err != nil {
		fmt.Printf("Error verifying credentials: %v\n", err)
		return
	}

	if verifyClient.Store.ID == nil {
		fmt.Println("Error: Device ID was not properly saved to database")
		return
	}

	fmt.Printf("Verification successful! Device ID: %s\n", verifyClient.Store.ID.String())
	fmt.Println("\nYou can now use 'go run main.go message' to listen for messages")
}
