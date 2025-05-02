package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

var (
	db            *sql.DB
	currentChatID string
	messages      []Message
	autoSave      bool = true
)

func main() {
	dbHost := flag.String("host", "localhost", "Database host")
	dbPort := flag.String("port", "5432", "Database port")
	dbName := flag.String("db", "dieter", "Database name")
	dbUser := flag.String("user", "prisma", "Database username")
	dbPass := flag.String("pass", "", "Database password")
	chatID := flag.String("id", "", "Chat ID to load")
	flag.Parse()

	myApp := app.New()
	myWindow := myApp.NewWindow("Chat Log Editor")

	hostEntry := widget.NewEntry()
	hostEntry.SetPlaceHolder("Host (e.g. localhost)")
	hostEntry.SetText(*dbHost)

	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("Port (e.g. 5432)")
	portEntry.SetText(*dbPort)

	dbNameEntry := widget.NewEntry()
	dbNameEntry.SetPlaceHolder("Database name")
	if *dbName != "" {
		dbNameEntry.SetText(*dbName)
	} else {
		dbNameEntry.SetText("dieter")
	}

	userEntry := widget.NewEntry()
	userEntry.SetPlaceHolder("Username")
	if *dbUser != "" {
		userEntry.SetText(*dbUser)
	} else {
		userEntry.SetText("prisma")
	}

	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("Password")
	if *dbPass != "" {
		passEntry.SetText(*dbPass)
	}

	chatIDEntry := widget.NewEntry()
	chatIDEntry.SetPlaceHolder("Chat ID")
	if *chatID != "" {
		chatIDEntry.SetText(*chatID)
	}

	// log widget
	logText := widget.NewRichText()
	logText.Wrapping = fyne.TextWrapWord

	logScroll := container.NewVScroll(logText)
	logScroll.SetMinSize(fyne.NewSize(200, 200))

	// add timestamped log entry
	addLogMessage := func(msg string) {
		timestamp := time.Now().Format("15:04:05")

		timestampSegment := &widget.TextSegment{
			Style: widget.RichTextStyle{
				TextStyle: fyne.TextStyle{Bold: true},
				ColorName: theme.ColorNameForeground,
			},
			Text: fmt.Sprintf("[%s] ", timestamp),
		}

		messageSegment := &widget.TextSegment{
			Style: widget.RichTextStyle{
				ColorName: theme.ColorNameForeground,
			},
			Text: msg,
		}

		newlineSegment := &widget.TextSegment{
			Text: "\n",
		}

		logText.Segments = append(logText.Segments, timestampSegment, messageSegment, newlineSegment)
		logText.Refresh()
		logScroll.ScrollToBottom()
	}

	addLogMessage("Application started")
	addLogMessage("Auto-save is enabled by default")

	// message editor components
	roleSelect := widget.NewSelect([]string{"user", "assistant"}, nil)
	contentEntry := widget.NewMultiLineEntry()
	contentEntry.SetPlaceHolder("Message content")

	var selectedMessageIndex int = -1

	// load message into editor
	loadMessageIntoEditor := func(index int) {
		if index >= 0 && index < len(messages) {
			selectedMessageIndex = index
			roleSelect.SetSelected(messages[index].Role)
			contentEntry.SetText(messages[index].Content)
		} else {
			selectedMessageIndex = -1
			roleSelect.SetSelected("")
			contentEntry.SetText("")
		}
	}

	// message list
	messageList := widget.NewList(
		func() int {
			return len(messages)
		},
		func() fyne.CanvasObject {
			roleLabel := widget.NewLabel("Role")
			roleLabel.TextStyle = fyne.TextStyle{Bold: true}

			contentPreview := widget.NewLabel("Content preview...")
			contentPreview.Wrapping = fyne.TextWrapWord

			return container.NewVBox(
				roleLabel,
				contentPreview,
				widget.NewSeparator(),
			)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			box := item.(*fyne.Container)
			roleLabel := box.Objects[0].(*widget.Label)
			contentPreview := box.Objects[1].(*widget.Label)

			roleLabel.SetText(strings.ToUpper(messages[id].Role))

			content := messages[id].Content
			if len(content) > 50 {
				content = content[:50] + "..."
			}
			contentPreview.SetText(content)
		},
	)

	messageList.OnSelected = func(id widget.ListItemID) {
		loadMessageIntoEditor(id)
	}

	// auto-save toggle
	autoSaveCheck := widget.NewCheck("Auto-save changes", func(checked bool) {
		autoSave = checked
		if checked {
			addLogMessage("Auto-save enabled")
		} else {
			addLogMessage("Auto-save disabled")
		}
	})
	autoSaveCheck.SetChecked(true)

	// save changes to database
	saveChanges := func() error {
		if db == nil || currentChatID == "" {
			return fmt.Errorf("no database connection or no chat loaded")
		}

		messagesJSON, err := json.MarshalIndent(messages, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to create JSON: %v", err)
		}

		_, err = db.Exec("UPDATE public.\"AuditChat\" SET messages = $1, length = $2 WHERE id = $3",
			string(messagesJSON), len(messages), currentChatID)
		if err != nil {
			return fmt.Errorf("failed to update chat: %v", err)
		}

		addLogMessage(fmt.Sprintf("Saved chat %s with %d messages", currentChatID, len(messages)))
		return nil
	}

	// load chat from database
	loadChatBtn := widget.NewButton("Load Chat", func() {
		if db == nil {
			dialog.ShowInformation("Error", "Please connect to database first", myWindow)
			return
		}

		id := chatIDEntry.Text
		currentChatID = id
		var messagesJSON string

		err := db.QueryRow("SELECT messages FROM public.\"AuditChat\" WHERE id = $1", id).Scan(&messagesJSON)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to load chat: %v", err)
			addLogMessage(errMsg)
			dialog.ShowError(fmt.Errorf(errMsg), myWindow)
			return
		}

		err = json.Unmarshal([]byte(messagesJSON), &messages)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to parse JSON: %v", err)
			addLogMessage(errMsg)
			dialog.ShowError(fmt.Errorf(errMsg), myWindow)
			return
		}

		messageList.Refresh()
		addLogMessage(fmt.Sprintf("Loaded chat %s with %d messages", id, len(messages)))
		loadMessageIntoEditor(-1)
	})

	// connect to database
	connectBtn := widget.NewButton("Connect to Database", func() {
		driverName := "postgres"
		connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			hostEntry.Text, portEntry.Text, userEntry.Text, passEntry.Text, dbNameEntry.Text)

		var err error
		db, err = sql.Open(driverName, connStr)
		if err != nil {
			errMsg := fmt.Sprintf("Connection failed: %v", err)
			addLogMessage(errMsg)
			dialog.ShowError(err, myWindow)
			return
		}

		err = db.Ping()
		if err != nil {
			errMsg := fmt.Sprintf("Connection failed: %v", err)
			addLogMessage(errMsg)
			dialog.ShowError(err, myWindow)
			return
		}

		addLogMessage(fmt.Sprintf("Connected to database %s@%s:%s", dbNameEntry.Text, hostEntry.Text, portEntry.Text))

		if chatIDEntry.Text != "" {
			loadChatBtn.OnTapped()
		}
	})

	// save chat button
	saveChatBtn := widget.NewButton("Save Chat", func() {
		err := saveChanges()
		if err != nil {
			errMsg := fmt.Sprintf("Save failed: %v", err)
			addLogMessage(errMsg)
			dialog.ShowError(fmt.Errorf(errMsg), myWindow)
		}
	})

	// create message from editor content
	createMessageFromEditor := func() (Message, error) {
		if roleSelect.Selected == "" {
			return Message{}, fmt.Errorf("please select a role")
		}

		if contentEntry.Text == "" {
			return Message{}, fmt.Errorf("please enter message content")
		}

		return Message{
			Role:    roleSelect.Selected,
			Content: contentEntry.Text,
		}, nil
	}

	// append message to end
	appendMessageBtn := widget.NewButton("Append", func() {
		newMsg, err := createMessageFromEditor()
		if err != nil {
			dialog.ShowInformation("Error", err.Error(), myWindow)
			return
		}

		messages = append(messages, newMsg)
		messageList.Refresh()

		newIndex := len(messages) - 1
		messageList.Select(newIndex)
		loadMessageIntoEditor(newIndex)

		addLogMessage(fmt.Sprintf("Appended new %s message", newMsg.Role))

		if autoSave {
			err := saveChanges()
			if err != nil {
				errMsg := fmt.Sprintf("Auto-save failed: %v", err)
				addLogMessage(errMsg)
				dialog.ShowError(fmt.Errorf(errMsg), myWindow)
			}
		}
	})

	// insert message at current position
	prependMessageBtn := widget.NewButton("Prepend", func() {
		newMsg, err := createMessageFromEditor()
		if err != nil {
			dialog.ShowInformation("Error", err.Error(), myWindow)
			return
		}

		insertPos := max(selectedMessageIndex, 0)

		messages = slices.Insert(messages, insertPos, newMsg)
		messageList.Refresh()

		messageList.Select(insertPos)
		loadMessageIntoEditor(insertPos)

		addLogMessage(fmt.Sprintf("Inserted new %s message at position %d", newMsg.Role, insertPos+1))

		if autoSave {
			err := saveChanges()
			if err != nil {
				errMsg := fmt.Sprintf("Auto-save failed: %v", err)
				addLogMessage(errMsg)
				dialog.ShowError(fmt.Errorf(errMsg), myWindow)
			}
		}
	})

	// update selected message
	updateMessageBtn := widget.NewButton("Update Message", func() {
		if selectedMessageIndex < 0 || selectedMessageIndex >= len(messages) {
			dialog.ShowInformation("Error", "No message selected", myWindow)
			return
		}

		newMsg, err := createMessageFromEditor()
		if err != nil {
			dialog.ShowInformation("Error", err.Error(), myWindow)
			return
		}

		messages[selectedMessageIndex] = newMsg
		messageList.Refresh()
		messageList.Select(selectedMessageIndex)

		addLogMessage(fmt.Sprintf("Updated message #%d", selectedMessageIndex+1))

		if autoSave {
			err := saveChanges()
			if err != nil {
				errMsg := fmt.Sprintf("Auto-save failed: %v", err)
				addLogMessage(errMsg)
				dialog.ShowError(fmt.Errorf(errMsg), myWindow)
			}
		}
	})

	// delete selected message
	deleteMessageBtn := widget.NewButton("Delete Message", func() {
		if selectedMessageIndex < 0 || selectedMessageIndex >= len(messages) {
			dialog.ShowInformation("Error", "No message selected", myWindow)
			return
		}

		currentIndex := selectedMessageIndex

		messages = slices.Delete(messages, currentIndex, currentIndex+1)
		messageList.Refresh()
		addLogMessage(fmt.Sprintf("Deleted message #%d", currentIndex+1))

		if len(messages) > 0 {
			if currentIndex >= len(messages) {
				currentIndex = len(messages) - 1
			}
			messageList.Select(currentIndex)
			loadMessageIntoEditor(currentIndex)
		} else {
			loadMessageIntoEditor(-1)
		}

		if autoSave {
			err := saveChanges()
			if err != nil {
				errMsg := fmt.Sprintf("Auto-save failed: %v", err)
				addLogMessage(errMsg)
				dialog.ShowError(fmt.Errorf(errMsg), myWindow)
			}
		}
	})

	// layout components
	dbForm := container.NewVBox(
		widget.NewLabel("Database Connection"),
		hostEntry,
		portEntry,
		dbNameEntry,
		userEntry,
		passEntry,
		connectBtn,
	)

	chatForm := container.NewVBox(
		widget.NewLabel("Chat Selection"),
		chatIDEntry,
		loadChatBtn,
		saveChatBtn,
		autoSaveCheck,
	)

	buttonContainer := container.NewHBox(
		prependMessageBtn,
		appendMessageBtn,
		updateMessageBtn,
		deleteMessageBtn,
	)

	editorHeader := container.NewVBox(
		widget.NewLabel("Message Editor"),
		container.NewHBox(widget.NewLabel("Role:"), roleSelect),
		widget.NewLabel("Content:"),
	)

	messageEditor := container.NewBorder(
		editorHeader,
		buttonContainer,
		nil,
		nil,
		container.NewVScroll(contentEntry),
	)

	logTitle := widget.NewLabel("Application Log")
	logTitle.TextStyle = fyne.TextStyle{Bold: true}

	leftPanel := container.NewBorder(
		container.NewVBox(
			dbForm,
			widget.NewSeparator(),
			chatForm,
			widget.NewSeparator(),
			logTitle,
		),
		nil,
		nil,
		nil,
		logScroll,
	)

	rightSplit := container.NewVSplit(
		container.NewVScroll(messageList),
		messageEditor,
	)
	rightSplit.Offset = 0.6 // 60% message list, 40% editor

	mainSplit := container.NewHSplit(
		leftPanel,
		rightSplit,
	)
	mainSplit.Offset = 0.25

	myWindow.SetContent(mainSplit)
	myWindow.Resize(fyne.NewSize(1000, 700))

	// auto-connect if credentials provided
	if *dbHost != "" && *dbPort != "" && *dbName != "" && *dbUser != "" && *dbPass != "" {
		addLogMessage("Auto-connecting with provided credentials")
		connectBtn.OnTapped()
	}

	myWindow.ShowAndRun()
}
