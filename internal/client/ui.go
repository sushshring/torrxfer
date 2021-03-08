package internal

import (
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/inhies/go-bytesize"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	torrxfer "github.com/sushshring/torrxfer/pkg/client"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/crypto"
)

var (
	name = "torrxfer-client"

	tviewApp               *tview.Application
	tvMainGrid             *tview.Grid
	tvMenu                 *tview.List
	tvAddServerForm        *tview.Form
	tvConnectionStatusGrid *tview.Grid

	tvConnectionStatusElements []tview.Primitive
	tvConnectionStatusMux      sync.Mutex

	client *torrxfer.TorrxferClient

	tableLogger *LogTable
)

/********* Client update logic **********/
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
			table.logTable.SetCell(table.logTable.GetRowCount(), 0,
				tview.NewTableCell(fmt.Sprintf("%s", p)).
					SetMaxWidth(0).
					SetAlign(tview.AlignCenter))
			table.logTable.ScrollToEnd()
		})
		return len(p), nil
	}
	return os.Stdout.Write(p)
}

// Helper thread only
func updateServerState(serverNotification torrxfer.ServerNotification) {
	log.Debug().Uint8("Notification: ", uint8(serverNotification.NotificationType)).Msg("Received server notification")
	{
		tvConnectionStatusMux.Lock()
		defer tvConnectionStatusMux.Unlock()
		switch serverNotification.NotificationType {
		case torrxfer.Connected:
			tvConnectionStatusElements = append(tvConnectionStatusElements, generateServerStatusUI(serverNotification.Connection))
			break

		case torrxfer.Disconnected:
			tvConnectionStatusElements = append(tvConnectionStatusElements[:serverNotification.Connection.Index], tvConnectionStatusElements[serverNotification.Connection.Index+1:]...)
			break
		}
	}
	rows := make([]int, serverNotification.Connection.Index+1)
	for i := range rows {
		rows[i] = 0
	}
	updateUI(func() {
		tvConnectionStatusGrid.Clear()
		tvConnectionStatusGrid.SetRows(rows...).
			AddItem(tvConnectionStatusElements[serverNotification.Connection.Index], int(serverNotification.Connection.Index), 0, 1, 1, 0, 0, false)
	})
}

/********* UI layouts ***********/
// Main thread only
func generateTextHeader(text string) tview.Primitive {
	return tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText(text)
}

// Main thread only
func generateServerStatusUI(server *torrxfer.ServerConnection) tview.Primitive {
	view := tview.NewFlex()
	view.SetDirection(tview.FlexRow)
	titleText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText(fmt.Sprintf("%s:%d", server.Address, server.Port))
	titleText.
		SetBackgroundColor(tcell.ColorDarkOrchid).
		SetBorderPadding(1, 1, 1, 1)
	view.AddItem(titleText, 0, 1, false).
		AddItem(tview.NewTextView().
			SetTextAlign(tview.AlignLeft).
			SetText((fmt.Sprintf("Connected since: %s", server.ConnectionTime.Local().String()))), 0, 1, false).
		AddItem(tview.NewTextView().
			SetTextAlign(tview.AlignLeft).
			SetText((fmt.Sprintf("Bytes transferred total: %s", bytesize.New(float64(server.BytesTransferred))))), 0, 1, false)

	for file, transferred := range server.FilesTransferred {
		var x int = int(transferred * 10 / file.Size)
		view.
			AddItem(tview.NewTextView().
				SetTextAlign(tview.AlignLeft).
				SetText((fmt.Sprintf("%s: %s%s %s/%s",
					file.Path,
					strings.Repeat("■", x),
					strings.Repeat("□", 10-x),
					bytesize.New(float64(transferred)),
					bytesize.New(float64(file.Size))))), 0, 1, false)
	}
	return view
}

func StartUI(c *torrxfer.TorrxferClient) {
	// Change the logging mechanism to write to the table instead of stdout
	tableLogger = &LogTable{}
	common.ChangeLogger(tableLogger, true)

	client = c
	tvConnectionStatusElements = make([]tview.Primitive, 0)
	// Create the main UI grid
	if tvMainGrid == nil {
		tvMainGrid = tview.NewGrid().
			SetRows(3, 0).
			SetColumns(0, 0).
			SetBorders(true).
			AddItem(generateTextHeader(name), 0, 0, 1, 2, 0, 0, false).
			// Create the connection status window
			AddItem(generateConnectionStatusUI(), 1, 0, 2, 1, 0, 0, false).
			// Create the main access menu
			AddItem(generateMenu(), 1, 1, 1, 1, 0, 0, true).
			// Create the log streaming window
			AddItem(generateLogStreaming(), 2, 1, 1, 1, 0, 0, false)
	}
	if tviewApp == nil {
		tviewApp = tview.NewApplication()
	}
	if err := tviewApp.SetRoot(tvMainGrid, true).Run(); err != nil {
		log.Panic().Err(err).Msg("Failed to create application")
	}
}

// Main thread only
func generateConnectionStatusUI() *tview.Grid {
	if tvConnectionStatusGrid == nil {
		tvConnectionStatusGrid = tview.NewGrid().
			SetBorders(true).
			SetRows(0).
			SetColumns(0)
		tvConnectionStatusGrid.SetTitle("Connection Status")

		go func() {
			for server := range client.RegisterForConnectionNotifications() {
				updateServerState(server)
			}
		}()
	}
	// for i in watched folders	UIs AddItem(i)
	return tvConnectionStatusGrid
}

// Main thread only
func generateLogStreaming() *tview.Table {
	logStreaming := tview.NewTable().SetBorders(true).SetSelectable(false, false)
	if tableLogger != nil {
		tableLogger.logTable = logStreaming
	}
	return logStreaming
}

// Main thread only
func generateMenu() *tview.List {
	if tvMenu == nil {
		tvMenu = tview.NewList().
			AddItem("Connect", "Create a connection to a new torrxfer server and transfer current files", 'c', connectServer).
			AddItem("Add folder", "Add new folder to client's watchlist", 'a', addDirectory).
			AddItem("Background", "Dismiss the UI and run in the background", 'b', nil).
			AddItem("Configuration", "Change configuration for client", 's', settings).
			AddItem("Quit", "Stop the application", 'q', func() {
				log.Debug().Msg("User called application quit")
				tviewApp.Stop()
			})
	}
	return tvMenu
}

// Main thread only
func connectServer() {
	log.Debug().Msg("Starting connection to server")
	var addServerForm *tview.Form
	var serverAddressField tview.FormItem
	var serverCertFileField tview.FormItem
	const (
		serverAddressLabel string = "Server Address"
		serverPortLabel    string = "Server port"
		certFileLabel      string = "Certificate file path"
		useTLSLabel        string = "Use secure connection?"
	)
	var certData *x509.Certificate
	certDataMux := sync.Mutex{}

	serverAddressField = tview.NewInputField().
		SetLabel(serverAddressLabel).
		SetText(common.DefaultAddress).
		SetFieldWidth(0).
		SetDoneFunc(func(key tcell.Key) {
			certData = nil
			textToCheck := addServerForm.GetFormItemByLabel(serverAddressLabel).(*tview.InputField).GetText()
			if net.ParseIP(textToCheck) == nil {
				// Mark field invalid
				updateUI(func() {
					serverAddressField.(*tview.InputField).SetBackgroundColor(tcell.ColorDarkRed)
				})
			}
			log.Debug().Str("Address", textToCheck).Msg("Getting cert for website")
			port, _ := strconv.ParseUint(addServerForm.GetFormItemByLabel(serverAddressLabel).(*tview.InputField).GetText(), 10, 32)
			if _, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", textToCheck, port)); err != nil {
				log.Error().Err(err).Msg("Could not resolve address")
			} else {
				resp, err := http.Head(fmt.Sprintf("https://%s", textToCheck))
				if err != nil {
					log.Info().Err(err).Msg("Failed to get cert for provided website")
				} else {
					log.Debug().Msg("Got cert data")
					certDataMux.Lock()
					defer certDataMux.Unlock()
					certData = nil
					certData = resp.TLS.PeerCertificates[0]
				}
				return
			}
			updateUI(func() {
				serverAddressField.(*tview.InputField).SetBackgroundColor(tcell.ColorDarkRed)
			})
		})
	serverCertFileField = tview.NewInputField().
		SetLabel(certFileLabel).
		SetFieldWidth(0).
		SetDoneFunc(func(key tcell.Key) {
			if !addServerForm.GetFormItemByLabel(useTLSLabel).(*tview.Checkbox).IsChecked() {
				updateUI(func() {
					serverCertFileField.(*tview.InputField).SetBackgroundColor(tcell.ColorDarkRed)
				})
			}
			// Already validated using network connection
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
						serverCertFileField.(*tview.InputField).SetBackgroundColor(tcell.ColorDarkRed)
					})
				}
				if !valid {
					log.Debug().Msg("Cert file not valid")
					updateUI(func() {
						serverCertFileField.(*tview.InputField).SetBackgroundColor(tcell.ColorDarkRed)
					})
				} else {
					log.Debug().Msg("Successfully validated cert file")
					certData = cert
					return
				}
			}
			log.Debug().Msg("Cert file not found")
			serverCertFileField.(*tview.InputField).SetBackgroundColor(tcell.ColorDarkRed)
		})
	addServerForm = tview.NewForm().
		SetFieldBackgroundColor(tcell.ColorDarkCyan).
		SetButtonBackgroundColor(tcell.ColorDarkSlateGray).
		AddFormItem(serverAddressField).
		SetButtonsAlign(tview.AlignCenter).
		AddInputField(serverPortLabel, fmt.Sprint(common.DefaultPort), 0, func(textToCheck string, lastChar rune) bool {
			if _, err := strconv.ParseUint(textToCheck, 10, 32); err != nil {
				return false
			}
			return true
		}, nil).
		AddFormItem(serverCertFileField).
		AddCheckbox(useTLSLabel, true, nil).
		AddButton("Quit", func() {
			log.Debug().Msg("Quit adding server")
			updateUI(func() {
				tvMainGrid.RemoveItem(addServerForm)
				tvMainGrid.AddItem(generateMenu(), 1, 1, 1, 1, 0, 0, true)
				tviewApp.SetFocus(generateMenu())
			})
		}).
		AddButton("Connect", func() {
			// Ignore parsing error since that should have been handled by the form validation
			port, _ := strconv.ParseUint(addServerForm.GetFormItemByLabel(serverPortLabel).(*tview.InputField).GetText(), 10, 32)

			// Should use TLS?
			var useTLS bool
			// If verification through HEAD request succeeded, use TLS by default
			if certData != nil {
				useTLS = true
			} else {
				useTLS = addServerForm.GetFormItemByLabel(useTLSLabel).(*tview.Checkbox).IsChecked()
			}

			if useTLS {
				// Invalid state as cert file should be valid or verified from network call
				if certData == nil {
					log.Fatal().Msg("Illegal state. Cert was validated but then not found????")
				}
			}

			server := &common.ServerConnectionConfig{
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
		}).
		SetFocus(0)
	addServerForm.SetTitle("Add Server").SetTitleAlign(tview.AlignCenter)
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
		addDirectoryForm    *tview.Form
		directoryInputField *tview.InputField
	)
	const watchDirectoryLabel string = "Add directory:"
	directoryInputField = tview.NewInputField().
		SetLabel(watchDirectoryLabel).
		SetFieldWidth(0).
		SetDoneFunc(func(key tcell.Key) {
			dirPath := addDirectoryForm.GetFormItemByLabel(watchDirectoryLabel).(*tview.InputField).GetText()
			if _, err := os.Stat(dirPath); os.IsNotExist(err) {
				updateUI(func() {
					directoryInputField.SetBackgroundColor(tcell.ColorDarkRed)
				})
			}
		})
	addDirectoryForm = tview.NewForm().
		SetFieldBackgroundColor(tcell.ColorDarkCyan).
		SetButtonBackgroundColor(tcell.ColorDarkSlateGray).
		AddFormItem(directoryInputField).
		SetButtonsAlign(tview.AlignCenter).
		AddButton("Quit", func() {
			log.Debug().Msg("Quit adding directory")
			updateUI(func() {
				tvMainGrid.RemoveItem(addDirectoryForm)
				tvMainGrid.AddItem(generateMenu(), 1, 1, 1, 1, 0, 0, true)
				tviewApp.SetFocus(generateMenu())
			})
		}).
		AddButton("Add", func() {
			dirPath := addDirectoryForm.GetFormItemByLabel(watchDirectoryLabel).(*tview.InputField).GetText()
			if err := client.WatchDirectory(dirPath); err != nil {
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
