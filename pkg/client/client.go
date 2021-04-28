package client

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/net"
)

// TorrxferClient struct describes the client functionality for Torrxfer
type TorrxferClient interface {
	WatchDirectory(dirname, mediaDirectoryRoot string) error
	ConnectServer(server common.ServerConnectionConfig) (*ServerConnection, error)
	RegisterForConnectionNotifications() <-chan ServerNotification
	Run(*os.File) error
}

type torrxferClient struct {
	connections          []*ServerConnection
	notificationChannels []chan ServerNotification
	fileStoredDbs        []FileWatcher
	jobQueue             chan<- ServerTransferJob
	clientConfig         *common.ClientConfig
	sync.RWMutex
}

// NewTorrxferClient creates and instantiates a torrxfer client struct
func NewTorrxferClient() (c TorrxferClient) {
	// Initialize from local db
	c = &torrxferClient{
		connections:          []*ServerConnection{},
		notificationChannels: []chan ServerNotification{},
		fileStoredDbs:        []FileWatcher{},
		jobQueue:             nil,
		RWMutex:              sync.RWMutex{},
	}
	return
}

func (c *torrxferClient) Run(config *os.File) error {
	jsonData, err := ioutil.ReadAll(config)
	if err != nil {
		common.LogErrorStack(err, "Failed to read config file")
		return err
	}

	var clientConfig common.ClientConfig
	err = json.Unmarshal(jsonData, &clientConfig)
	log.Debug().RawJSON("", jsonData).Msg("JSON data: ")
	if err != nil {
		common.LogErrorStack(err, "Failed to unmarshal json")
		return err
	}

	for _, serverConfig := range clientConfig.Servers {
		log.Debug().Str("Address", serverConfig.Address).Uint32("Port", serverConfig.Port).Msg("Connecting to server")
		server, err := c.ConnectServer(serverConfig)
		if err != nil {
			log.Error().Err(err).Str("Server address", serverConfig.Address).Msg("Failed to connect to server")
			continue
		}
		c.connections = append(c.connections, server)
	}

	for _, dir := range clientConfig.WatchedDirectories {
		err = c.WatchDirectory(dir.Directory, dir.MediaRoot)
		if err != nil {
			log.Error().Stack().Err(err).Str("Directory: ", dir.Directory).Msg("Could not watch directory")
		}
	}
	c.clientConfig = &clientConfig

	// Create job queue
	jobQueue := make(chan ServerTransferJob, 100)
	c.jobQueue = jobQueue

	// Start the dispatcher.
	dispatcher := NewDispatcher(jobQueue, 5)
	dispatcher.run()

	go func() {
		doneChan := c.configureSignals()
		<-doneChan
		for _, fileWatcher := range c.fileStoredDbs {
			fileWatcher.Close()
		}

		for _, notificationChan := range c.notificationChannels {
			close(notificationChan)
		}
		close(c.jobQueue)
	}()

	go func() {
		for notification := range c.RegisterForConnectionNotifications() {
			if notification.Error != nil {
				// Touch the file to requeue a transfer
				os.Chtimes(notification.SentFile.Path, time.Now(), time.Now())
			}
			log.Trace().
				Str("Notification: ", ConnectionNotificationStrings[notification.NotificationType]).
				Uint64("Bytes transferred: ", notification.Connection.bytesTransferred).
				Str("To: ", notification.Connection.address).
				Msg("Connection update")
		}
	}()
	return nil
}

// WatchDirectory watches a provided directory for changes and returns a channel the yields filepaths
// Currently the client does not support retroactively sending watched files to a new server connection
// If a new server connection is made, it will only get updates for files that are created or written to
// after the connection starts
func (c *torrxferClient) WatchDirectory(dirname, mediaDirectoryRoot string) error {
	log.Debug().Str("Adding directory", dirname).Send()
	fileWatcher, err := NewFileWatcher(dirname, mediaDirectoryRoot)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to create file watcher")
		return err
	}
	func() {
		c.Lock()
		defer c.Unlock()
		c.fileStoredDbs = append(c.fileStoredDbs, fileWatcher)
	}()
	// Start listening for files to be transferred
	go func() {
		for file := range fileWatcher.RegisterForFileNotifications() {
			log.Trace().Str("Name", file.Path).Msg("Attempting to transfer file.")
			c.transferToServers(file)
		}
	}()
	return nil
}

// ConnectServer creates a connection to the server provided
func (c *torrxferClient) ConnectServer(server common.ServerConnectionConfig) (*ServerConnection, error) {
	// Connect to the server
	rpc, err := net.NewTorrxferServerConnection(server)
	if err != nil {
		common.LogError(err, "RPC connection failed")
		return nil, err
	}

	serverConnection := newServerConnection(uint16(len(c.connections)), server.Address, server.Port, rpc)

	return serverConnection, nil
}

// RegisterForConnectionNotifications is a client method that notifies the caller on changes to all active connections
func (c *torrxferClient) RegisterForConnectionNotifications() <-chan ServerNotification {
	channel := make(chan ServerNotification, 500)
	c.notificationChannels = append(c.notificationChannels, channel)
	return channel
}

func (c *torrxferClient) configureSignals() chan bool {
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL, syscall.SIGHUP, syscall.SIGABRT)
	go func() {
		sig := <-sigs
		log.Trace().Str("Signal", sig.String()).Msg("Received int")
		done <- true
	}()
	return done
}

// transferToServers reads a provided file and transfers it to the connected servers
func (c *torrxferClient) transferToServers(file *File) {
	// Send file to all connected servers
	c.RLock()
	defer c.RUnlock()

	for _, server := range c.connections {
		log.Trace().Str("Path", file.Path).Str("Address", server.address).Msg("Starting file transfer to server")
		transferJob := ServerTransferJob{
			ID:                    uuid.New(),
			Delay:                 0,
			ServerConnection:      server,
			File:                  file,
			TransferNotifications: make(chan ServerNotification),
		}
		c.jobQueue <- transferJob
		go func() {
			for notification := range transferJob.TransferNotifications {
				switch notification.NotificationType {
				// Retry transfer on error
				case ConnectionNotificationTypeQueryError:
					fallthrough
				case ConnectionNotificationTypeTransferError:
					log.Debug().Err(notification.Error)
					c.jobQueue <- transferJob
				case ConnectionNotificationTypeCompleted:
					if c.clientConfig.DeleteOnComplete {
						os.Remove(notification.SentFile.Path)
					}
					fallthrough
				// Pipe other notifications to subscribers
				default:
					for _, subscriber := range c.notificationChannels {
						subscriber <- notification
					}
				}
			}
		}()
	}
}
