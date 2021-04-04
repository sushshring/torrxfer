package internal

import (
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/inhies/go-bytesize"
	tview "gitlab.com/tslocum/cview"

	// "github.com/rivo/tview"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	torrxfer "github.com/sushshring/torrxfer/pkg/client"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/crypto"
)

var (
	tviewApp   *tview.Application
	tvMainGrid *tview.Grid
	tvMenu     *tview.List
	// tvAddServerForm        *tview.Form
	tvConnectionStatusGrid *tview.Grid

	tvConnectionStatusElements []tview.Primitive
	tvConnectionStatusMux      sync.RWMutex

	progressBarsMutex sync.RWMutex
	progressBars      []map[string]*tview.ProgressBar

	client torrxfer.TorrxferClient

	tableLogger *LogTable
)

const (
	name = "torrxfer-client"

	// progressBarPixels = 100
)

/********* Client update logic **********/

// LogTable is a struct that embeds the tview table and implements io.Writer for logging in the tview UI
type LogTable struct {
	logTable *tview.Table
}

func updateUI(f func()) {
	go func() {
		tviewApp.QueueUpdateDraw(f)
	}()
}

// No log in this to prevent infinite recursion
func (table *LogTable) Write(p []byte) (n int, err error) {
	if table.logTable != nil {
		updateUI(func() {
			cell := tview.NewTableCell(fmt.Sprintf("%s", p))
			cell.SetMaxWidth(0)
			cell.SetExpansion(1)
			table.logTable.SetCell(table.logTable.GetRowCount(), 0, cell)
			table.logTable.ScrollToEnd()
		})
		return len(p), nil
	}
	return os.Stdout.Write(p)
}

// Helper thread only
func updateServerState(serverNotification torrxfer.ServerNotification) {
	log.Debug().Uint8("Notification: ", uint8(serverNotification.NotificationType)).Msg("Received server notification")
	func() {
		tvConnectionStatusMux.Lock()
		defer tvConnectionStatusMux.Unlock()
		switch serverNotification.NotificationType {
		case torrxfer.ConnectionNotificationTypeConnected:
			tvConnectionStatusElements = append(tvConnectionStatusElements, generateServerStatusUI(serverNotification.Connection))

		case torrxfer.ConnectionNotificationTypeDisconnected:
			tvConnectionStatusElements = append(tvConnectionStatusElements[:serverNotification.Connection.GetIndex()], tvConnectionStatusElements[serverNotification.Connection.GetIndex()+1:]...)
		case torrxfer.ConnectionNotificationTypeFilesUpdated:
			updateUI(func() {
				for _, file := range serverNotification.Connection.GetFilesTransferred() {
					progressBarsMutex.RLock()
					defer progressBarsMutex.RUnlock()
					log.Debug().Str("File", file.Path).Int("Progress", int(serverNotification.Connection.GetFileSizeOnServer(file.Path))).Msg("Progress update")
					progress := progressBars[serverNotification.Connection.GetIndex()][file.Path]
					currentProgress := int(serverNotification.Connection.GetFileSizeOnServer(file.Path))
					progress.SetProgress(currentProgress)
				}
			})
			return
		}
	}()
	rows := make([]int, len(tvConnectionStatusElements))
	for i := range rows {
		rows[i] = 0
	}
	updateUI(func() {
		tvConnectionStatusMux.RLock()
		defer tvConnectionStatusMux.RUnlock()
		tvConnectionStatusGrid.SetRows(rows...)
		for i := range tvConnectionStatusElements {
			tvConnectionStatusGrid.AddItem(tvConnectionStatusElements[i], i, 0, 1, 1, 0, 0, false)
		}
	})
}

/********* UI layouts ***********/
// Main thread only
func generateTextHeader(text string) tview.Primitive {
	textView := tview.NewTextView()
	textView.SetTextAlign(tview.AlignCenter)
	textView.SetText(text)
	return textView
}

// Main thread only
func generateServerStatusUI(server *torrxfer.ServerConnection) tview.Primitive {
	view := tview.NewFlex()
	view.SetDirection(tview.FlexRow)

	titleText := tview.NewTextView()
	titleText.SetTextAlign(tview.AlignCenter)
	titleText.SetText(fmt.Sprintf("%s:%d", server.GetAddress(), server.GetPort()))
	titleText.SetBackgroundColor(tcell.ColorDarkOrchid)
	titleText.SetPadding(1, 1, 1, 1)
	view.AddItem(titleText, 0, 1, false)

	connectedSinceTextView := tview.NewTextView()
	connectedSinceTextView.SetText((fmt.Sprintf("Connected since: %s", server.GetConnectionTime().Local().String())))
	view.AddItem(connectedSinceTextView, 0, 1, false)

	bytesTransferredTextView := tview.NewTextView()
	bytesTransferredTextView.SetText(fmt.Sprintf("Bytes transferred total: %s", bytesize.New(float64(server.GetBytesTransferred()))))
	view.AddItem(bytesTransferredTextView, 0, 1, false)

	progressBars = append(progressBars, make(map[string]*tview.ProgressBar))
	progressBarsMutex.Lock()
	defer progressBarsMutex.Unlock()
	for _, file := range server.GetFilesTransferred() {
		progress := tview.NewProgressBar()
		progress.SetFilledRune('■')
		progress.SetEmptyRune('□')
		progress.SetMax(int(file.Size))

		progressBars[server.GetIndex()][file.Path] = progress
		view.AddItem(progress, 0, 1, false)
	}
	return view
}

// StartUI runs the client UI in the attached tty
func StartUI(c torrxfer.TorrxferClient) {
	// Change the logging mechanism to write to the table instead of stdout
	tableLogger = &LogTable{}
	common.AddLogger(tableLogger, true)

	client = c
	tvConnectionStatusElements = make([]tview.Primitive, 0)
	// Create the main UI grid
	if tvMainGrid == nil {
		tvMainGrid = tview.NewGrid()
		tvMainGrid.SetRows(3, 0)
		tvMainGrid.SetColumns(0, 0)
		tvMainGrid.SetBorders(true)
		tvMainGrid.AddItem(generateTextHeader(name), 0, 0, 1, 2, 0, 0, false)
		// Create the connection status window
		tvMainGrid.AddItem(generateConnectionStatusUI(), 1, 0, 1, 1, 0, 0, false)
		// Create the main access menu
		tvMainGrid.AddItem(generateMenu(), 1, 1, 1, 1, 0, 0, true)
		// Create the log streaming window
		tvMainGrid.AddItem(generateLogStreaming(), 2, 0, 1, 2, 0, 0, false)
	}
	// Run connection updates
	go func() {
		for server := range client.RegisterForConnectionNotifications() {
			files := zerolog.Arr()
			for _, file := range server.Connection.GetFilesTransferred() {
				files.Str(file.Path)
			}
			log.Debug().
				Str("Connection updated", server.Connection.GetAddress()).
				Uint64("Transferred bytes", server.Connection.GetBytesTransferred()).
				Array("Files", files).
				Msg("")
			updateServerState(server)
		}
	}()
	if tviewApp == nil {
		tviewApp = tview.NewApplication()
	}
	tviewApp.SetRoot(tvMainGrid, true)
	if err := tviewApp.Run(); err != nil {
		log.Panic().Err(err).Msg("Failed to create application")
	}
}

// Main thread only
func generateConnectionStatusUI() *tview.Grid {
	if tvConnectionStatusGrid == nil {
		tvConnectionStatusGrid = tview.NewGrid()
		tvConnectionStatusGrid.SetBorders(true)
		tvConnectionStatusGrid.SetRows(0)
		tvConnectionStatusGrid.SetColumns(0)
		tvConnectionStatusGrid.SetTitle("Connection Status")
	}
	return tvConnectionStatusGrid
}

// Main thread only
func generateLogStreaming() *tview.Table {
	logStreaming := tview.NewTable()
	logStreaming.SetBorders(true)
	logStreaming.SetSelectable(false, false)
	if tableLogger != nil {
		tableLogger.logTable = logStreaming
	}
	return logStreaming
}

func generateListItem(mainString string, secondString string, shortcut rune, selectedFunc func()) *tview.ListItem {
	listItem := tview.NewListItem(mainString)
	listItem.SetSecondaryText(secondString)
	listItem.SetShortcut(shortcut)
	listItem.SetSelectedFunc(selectedFunc)
	return listItem
}

// Main thread only
func generateMenu() *tview.List {
	if tvMenu == nil {
		tvMenu = tview.NewList()
		tvMenu.AddItem(generateListItem("Connect", "Create a connection to a new torrxfer server and transfer current files", 'c', connectServer))
		tvMenu.AddItem(generateListItem("Add folder", "Add new folder to client's watchlist", 'a', addDirectory))
		tvMenu.AddItem(generateListItem("Background", "Dismiss the UI and run in the background", 'b', nil))
		tvMenu.AddItem(generateListItem("Configuration", "Change configuration for client", 's', settings))
		tvMenu.AddItem(generateListItem("Quit", "Stop the application", 'q', func() {
			log.Debug().Msg("User called application quit")
			tviewApp.Stop()
		}))
	}
	return tvMenu
}

// Main thread only
func connectServer() {
	log.Debug().Msg("Starting connection to server")
	var addServerForm *tview.Form
	const (
		serverAddressLabel string = "Server Address"
		serverPortLabel    string = "Server port"
		certFileLabel      string = "Certificate file path"
		useTLSLabel        string = "Use secure connection?"
	)
	var certData *x509.Certificate
	certDataMux := sync.RWMutex{}

	serverAddressField := tview.NewInputField()
	serverAddressField.SetLabel(serverAddressLabel)
	serverAddressField.SetText(common.DefaultAddress)
	serverAddressField.SetFieldWidth(0)
	serverAddressField.SetDoneFunc(func(key tcell.Key) {
		certData = nil
		textToCheck := addServerForm.GetFormItemByLabel(serverAddressLabel).(*tview.InputField).GetText()
		if net.ParseIP(textToCheck) == nil {
			// Mark field invalid
			updateUI(func() {
				serverAddressField.SetBackgroundColor(tcell.ColorDarkRed)
			})
		}
		log.Debug().Str("Address", textToCheck).Msg("Getting cert for website")
		port, _ := strconv.ParseUint(addServerForm.GetFormItemByLabel(serverAddressLabel).(*tview.InputField).GetText(), 10, 32)
		if _, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", textToCheck, port)); err != nil {
			common.LogErrorStack(err, "Could not resolve address")
		} else {
			resp, err := http.Head(fmt.Sprintf("https://%s", textToCheck))
			if err != nil {
				log.Info().Err(err).Msg("Failed to get cert for provided website")
			} else {
				func() {
					log.Debug().Msg("Got cert data")
					certDataMux.Lock()
					defer certDataMux.Unlock()
					certData = nil
					certData = resp.TLS.PeerCertificates[0]
				}()
			}
			return
		}
		updateUI(func() {
			serverAddressField.SetBackgroundColor(tcell.ColorDarkRed)
		})
	})
	serverCertFileField := tview.NewInputField()
	serverCertFileField.SetLabel(certFileLabel)
	serverCertFileField.SetFieldWidth(0)
	serverCertFileField.SetDoneFunc(func(key tcell.Key) {
		if !addServerForm.GetFormItemByLabel(useTLSLabel).(*tview.CheckBox).IsChecked() {
			updateUI(func() {
				serverCertFileField.SetBackgroundColor(tcell.ColorDarkRed)
			})
		}
		// Already validated using network connection
		certDataMux.RLock()
		defer certDataMux.RUnlock()
		if certData != nil {
			return
		}
		filePath := addServerForm.GetFormItemByLabel(certFileLabel).(*tview.InputField).GetText()
		if _, err := os.Stat(filePath); os.IsExist(err) {
			log.Debug().Str("Cert File", filePath).Msg("Validating x509 file")
			valid, cert, err := crypto.VerifyCert(filePath, addServerForm.GetFormItemByLabel(serverAddressLabel).(*tview.InputField).GetText())
			if err != nil {
				common.LogError(err, "Cert file not valid")
				updateUI(func() {
					serverCertFileField.SetBackgroundColor(tcell.ColorDarkRed)
				})
			}
			if !valid {
				log.Debug().Msg("Cert file not valid")
				updateUI(func() {
					serverCertFileField.SetBackgroundColor(tcell.ColorDarkRed)
				})
			} else {
				func() {
					certDataMux.Lock()
					defer certDataMux.Unlock()
					log.Debug().Msg("Successfully validated cert file")
					certData = cert
				}()
				return
			}
		}
		log.Debug().Msg("Cert file not found")
		serverCertFileField.SetBackgroundColor(tcell.ColorDarkRed)
	})
	addServerForm = tview.NewForm()
	addServerForm.SetFieldBackgroundColor(tcell.ColorDarkCyan)
	addServerForm.SetButtonBackgroundColor(tcell.ColorDarkSlateGray)
	addServerForm.AddFormItem(serverAddressField)
	addServerForm.SetButtonsAlign(tview.AlignCenter)
	addServerForm.AddInputField(serverPortLabel, fmt.Sprint(common.DefaultPort), 0, func(textToCheck string, lastChar rune) bool {
		if _, err := strconv.ParseUint(textToCheck, 10, 32); err != nil {
			return false
		}
		return true
	}, nil)
	addServerForm.AddFormItem(serverCertFileField)
	addServerForm.AddCheckBox(useTLSLabel, "", true, nil)
	addServerForm.AddButton("Quit", func() {
		log.Debug().Msg("Quit adding server")
		updateUI(func() {
			tvMainGrid.RemoveItem(addServerForm)
			tvMainGrid.AddItem(generateMenu(), 1, 1, 1, 1, 0, 0, true)
			tviewApp.SetFocus(generateMenu())
		})
	})
	addServerForm.AddButton("Connect", func() {
		// Ignore parsing error since that should have been handled by the form validation
		port, _ := strconv.ParseUint(addServerForm.GetFormItemByLabel(serverPortLabel).(*tview.InputField).GetText(), 10, 32)

		// Should use TLS?
		var useTLS bool
		certDataMux.RLock()
		defer certDataMux.RUnlock()
		// If verification through HEAD request succeeded, use TLS by default
		if certData != nil {
			useTLS = true
		} else {
			useTLS = addServerForm.GetFormItemByLabel(useTLSLabel).(*tview.CheckBox).IsChecked()
		}

		if useTLS {
			// Invalid state as cert file should be valid or verified from network call
			if certData == nil {
				log.Fatal().Msg("Illegal state. Cert was validated but then not found????")
			}
		}

		server := common.ServerConnectionConfig{
			Address:  addServerForm.GetFormItemByLabel(serverAddressLabel).(*tview.InputField).GetText(),
			Port:     uint32(port),
			UseTLS:   useTLS,
			CertFile: certData,
		}

		log.Debug().Str("", fmt.Sprintf("Addr: %s, port: %d, tls: %t", server.Address, server.Port, server.UseTLS)).Msg("Attempting to connect to")
		if _, err := client.ConnectServer(server); err != nil {
			common.LogError(err, "Connect server failed")
		}
		updateUI(func() {
			tvMainGrid.RemoveItem(addServerForm)
			tvMainGrid.AddItem(generateMenu(), 1, 1, 1, 1, 0, 0, true)
			tviewApp.SetFocus(generateMenu())
		})
	})
	addServerForm.SetFocus(0)
	addServerForm.SetTitle("Add Server")
	addServerForm.SetTitleAlign(tview.AlignCenter)
	updateUI(func() {
		tvMainGrid.RemoveItem(generateMenu())
		tvMainGrid.AddItem(addServerForm, 1, 1, 1, 1, 0, 0, true)
		tviewApp.SetFocus(addServerForm)
	})
}

// Main thread only
func addDirectory() {
	log.Debug().Msg("Adding a file to the watchlist")
	var (
		addDirectoryForm             *tview.Form
		directoryInputField          *tview.InputField
		mediaRootDirectoryInputField *tview.InputField
	)
	const watchDirectoryLabel string = "Add directory:"
	const mediaRootDirectoryLabel string = "Media root directory:"
	directoryInputField = tview.NewInputField()
	directoryInputField.SetLabel(watchDirectoryLabel)
	directoryInputField.SetFieldWidth(0)
	directoryInputField.SetDoneFunc(func(key tcell.Key) {
		dirPath := addDirectoryForm.GetFormItemByLabel(watchDirectoryLabel).(*tview.InputField).GetText()
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			updateUI(func() {
				directoryInputField.SetBackgroundColor(tcell.ColorDarkRed)
			})
		}
	})

	mediaRootDirectoryInputField = tview.NewInputField()
	mediaRootDirectoryInputField.SetLabel(mediaRootDirectoryLabel)
	mediaRootDirectoryInputField.SetFieldWidth(0)
	mediaRootDirectoryInputField.SetDoneFunc(func(key tcell.Key) {
		dirPath := addDirectoryForm.GetFormItemByLabel(watchDirectoryLabel).(*tview.InputField).GetText()
		mediaDirPath := addDirectoryForm.GetFormItemByLabel(mediaRootDirectoryLabel).(*tview.InputField).GetText()
		if _, err := os.Stat(dirPath); os.IsNotExist(err) || !common.IsSubdir(mediaDirPath, dirPath) {
			updateUI(func() {
				mediaRootDirectoryInputField.SetBackgroundColor(tcell.ColorDarkRed)
			})
		}
	})
	addDirectoryForm = tview.NewForm()
	addDirectoryForm.SetFieldBackgroundColor(tcell.ColorDarkCyan)
	addDirectoryForm.SetButtonBackgroundColor(tcell.ColorDarkSlateGray)
	addDirectoryForm.AddFormItem(directoryInputField)
	addDirectoryForm.AddFormItem(mediaRootDirectoryInputField)
	addDirectoryForm.SetButtonsAlign(tview.AlignCenter)
	addDirectoryForm.AddButton("Quit", func() {
		log.Debug().Msg("Quit adding directory")
		updateUI(func() {
			tvMainGrid.RemoveItem(addDirectoryForm)
			tvMainGrid.AddItem(generateMenu(), 1, 1, 1, 1, 0, 0, true)
			tviewApp.SetFocus(generateMenu())
		})
	})
	addDirectoryForm.AddButton("Add", func() {
		dirPath := addDirectoryForm.GetFormItemByLabel(watchDirectoryLabel).(*tview.InputField).GetText()
		mediaDirectoryRoot := addDirectoryForm.GetFormItemByLabel(mediaRootDirectoryLabel).(*tview.InputField).GetText()
		if err := client.WatchDirectory(dirPath, mediaDirectoryRoot); err != nil {
			common.LogError(err, "Could not watch directory")
		} else {
			updateUI(func() {
				tvMainGrid.RemoveItem(addDirectoryForm)
				tvMainGrid.AddItem(generateMenu(), 1, 1, 1, 1, 0, 0, true)
				tviewApp.SetFocus(generateMenu())
			})
		}
	})
	addDirectoryForm.SetTitle("Watch directory")
	updateUI(func() {
		tvMainGrid.RemoveItem(generateMenu())
		tvMainGrid.AddItem(addDirectoryForm, 1, 1, 1, 1, 0, 0, true)
		tviewApp.SetFocus(addDirectoryForm)
	})
}

// Main thread only
func settings() {
	log.Debug().Msg("Changing settings")
	// var changeSettingsForm *tview.Form
}
