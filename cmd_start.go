//+build !test

package appy

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/99designs/gqlgen/api"
	gqlgenCfg "github.com/99designs/gqlgen/codegen/config"
	"github.com/gorilla/websocket"
	"github.com/radovskyb/watcher"
	"go.uber.org/zap"
)

var (
	apiServeCmd                         *exec.Cmd
	webServeCmd                         *exec.Cmd
	webServeCmdReady                    chan os.Signal
	isGenerating                                      = false
	watcherPollInterval                 time.Duration = 1
	liveReloadWSConn, liveReloadWSSConn *websocket.Conn
)

func newStartCommand(logger *Logger, server *Server) *Command {
	return &Command{
		Use:   "start",
		Short: "Run the HTTP/HTTPS web server with `webpack-dev-server` in development watch mode (only available in debug build)",
		Run: func(cmd *Command, args []string) {
			if len(server.Config().Errors()) > 0 {
				logger.Fatal(server.Config().Errors()[0])
			}

			if server.Config().HTTPSSLEnabled && !server.IsSSLCertExisted() {
				logger.Fatal("HTTP_SSL_ENABLED is set to true without SSL certs, please generate using `ssl:setup` first.")
			}

			wd, _ := os.Getwd()
			watchPaths := []string{
				wd + "/assets",
				wd + "/cmd",
				wd + "/configs",
				wd + "/db",
				wd + "/pkg",
				wd + "/go.sum",
				wd + "/go.mod",
				wd + "/main.go",
			}
			quit := make(chan os.Signal, 1)
			webServeCmdReady = make(chan os.Signal, 1)

			signal.Notify(quit, os.Interrupt)
			signal.Notify(quit, syscall.SIGTERM)

			go func() {
				<-quit
				killWebServeCmd()
				killAPIServeCmd()
			}()

			if _, err := os.Stat(wd + "/package.json"); !os.IsNotExist(err) {
				go runWebServeCmd(logger, server)
			}

			go func() {
				<-webServeCmdReady
				runAPIServeCmd(logger)
			}()

			go func() {
				runLiveReloadServer(logger, server)
			}()

			watch(logger, watchPaths, func(e watcher.Event) {
				watchHandler(e, logger)
			})
		},
	}
}

func watchHandler(e watcher.Event, logger *Logger) {
	if isGenerating {
		return
	}

	isGenerating = true
	if strings.Contains(e.Path, ".gql") || strings.Contains(e.Path, ".graphql") || strings.Contains(e.Path, "pkg/graphql/config.yml") {
		logger.Info("* Generating GraphQL boilerplate code...")

		err := generateGQL()
		if err != nil {
			logger.Info(err.Error())
		}

		isGenerating = false
		return
	}

	gqlgenConfig, _ := gqlgenLoadConfig()
	if gqlgenConfig != nil && (strings.Contains(e.Path, gqlgenConfig.Model.Filename) || (strings.Contains(e.Path, gqlgenConfig.Exec.Filename) && e.Op == watcher.Remove)) {
		isGenerating = false
		return
	}

	isGenerating = false
	go runAPIServeCmd(logger)
}

func gqlgenLoadConfig() (*gqlgenCfg.Config, error) {
	wd, _ := os.Getwd()
	return gqlgenCfg.LoadConfig(wd + "/pkg/graphql/config.yml")
}

func generateGQL() error {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(os.Stderr)
	}()

	gqlgenConfig, _ := gqlgenLoadConfig()
	return api.Generate(gqlgenConfig)
}

func killAPIServeCmd() {
	if apiServeCmd != nil {
		syscall.Kill(-apiServeCmd.Process.Pid, syscall.SIGINT)
		apiServeCmd = nil
	}
}

func runAPIServeCmd(logger *Logger) {
	killAPIServeCmd()
	time.Sleep(500 * time.Millisecond)
	apiServeCmd = exec.Command("go", "run", ".", "serve")
	apiServeCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	apiServeCmd.Stdout = os.Stdout
	apiServeCmd.Stderr = os.Stderr

	go func() {
		time.Sleep(4000 * time.Millisecond)

		if liveReloadWSConn != nil {
			liveReloadWSConn.WriteMessage(websocket.TextMessage, []byte("reload"))
		}

		if liveReloadWSSConn != nil {
			liveReloadWSSConn.WriteMessage(websocket.TextMessage, []byte("reload"))
		}
	}()

	logger.Info("* Compiling...")
	apiServeCmd.Run()
}

func killWebServeCmd() {
	if webServeCmd != nil {
		syscall.Kill(-webServeCmd.Process.Pid, syscall.SIGINT)
		webServeCmd = nil
	}
}

func runWebServeCmd(logger *Logger, server *Server) {
	killWebServeCmd()
	wd, _ := os.Getwd()
	ssrPaths := []string{}
	for _, route := range server.Routes() {
		if route.Method == "GET" {
			ssrPaths = append(ssrPaths, route.Path)
		}
	}

	webServeCmd = exec.Command("npm", "start")
	webServeCmd.Dir = wd
	webServeCmd.Env = os.Environ()
	webServeCmd.Env = append(webServeCmd.Env, "APPY_SSR_ROUTES="+strings.Join(ssrPaths, ","))
	webServeCmd.Env = append(webServeCmd.Env, "HTTP_HOST="+server.Config().HTTPHost)
	webServeCmd.Env = append(webServeCmd.Env, "HTTP_PORT="+server.Config().HTTPPort)
	webServeCmd.Env = append(webServeCmd.Env, "HTTP_SSL_PORT="+server.Config().HTTPSSLPort)
	webServeCmd.Env = append(webServeCmd.Env, "HTTP_SSL_ENABLED="+strconv.FormatBool(server.Config().HTTPSSLEnabled))
	webServeCmd.Env = append(webServeCmd.Env, "HTTP_SSL_CERT_PATH="+server.Config().HTTPSSLCertPath)
	webServeCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	webServeCmdOut, _ := webServeCmd.StdoutPipe()
	webServeCmdErr, _ := webServeCmd.StderrPipe()

	go func(stdout io.ReadCloser) {
		defer func() {
			if r := recover(); r != nil {
				killWebServeCmd()
				killAPIServeCmd()
				logger.Fatal(r)
			}
		}()

		timeRe := regexp.MustCompile(` [0-9]+ms`)
		isFirstTime := true
		isWDSCompiling := false
		out := bufio.NewScanner(stdout)

		for out.Scan() {
			outText := strings.Trim(out.Text(), " ")

			if outText == "" && (isWDSCompiling || isFirstTime) {
				continue
			}

			if strings.Contains(outText, "｢wdm｣") || strings.HasPrefix(outText, "> ") || (isWDSCompiling && strings.Contains(outText, "｢wds｣")) {
				continue
			}

			if strings.Contains(outText, "Compiling...") || strings.Contains(outText, "｢wds｣") {
				isWDSCompiling = true
				logger.Info("* [wds] Compiling...")
			} else if strings.Contains(outText, "Compiled successfully in") {
				isWDSCompiling = false
				logger.Infof("* [wds] Compiled successfully in%s", timeRe.FindStringSubmatch(outText)[0])

				if isFirstTime {
					isFirstTime = false
					close(webServeCmdReady)
				}
			} else if strings.HasPrefix(outText, "ERROR  Failed to compile") {
				logger.Info("* [wds] Failed to compile.")
				logger.Info("")
			} else {
				if len(outText) > 0 {
					logger.Info(outText)
				}
			}
		}
	}(webServeCmdOut)

	go func(stderr io.ReadCloser) {
		defer func() {
			if r := recover(); r != nil {
				killWebServeCmd()
				killAPIServeCmd()
				logger.Fatal(r)
			}
		}()

		err := bufio.NewScanner(stderr)
		fatalErr := ""
		for err.Scan() {
			fatalErr = fatalErr + strings.Trim(err.Text(), " ") + "\n\t"
		}

		killWebServeCmd()
		killAPIServeCmd()
		time.Sleep(1 * time.Second)

		if fatalErr != "" {
			logger.Fatal(fatalErr)
		}
	}(webServeCmdErr)

	webServeCmd.Run()
}

func runLiveReloadServer(logger *Logger, server *Server) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	wsHandler := http.NewServeMux()
	wsHandler.HandleFunc(LiveReloadPath, func(w http.ResponseWriter, r *http.Request) {
		var err error

		liveReloadWSConn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			killWebServeCmd()
			killAPIServeCmd()
			logger.Fatal(err)
		}

		for {
			_, _, err := liveReloadWSConn.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	ws := &http.Server{
		Addr:    server.Config().HTTPHost + ":" + LiveReloadWSPort,
		Handler: wsHandler,
	}
	ws.ErrorLog = zap.NewStdLog(logger.Desugar())

	wssHandler := http.NewServeMux()
	wssHandler.HandleFunc(LiveReloadPath, func(w http.ResponseWriter, r *http.Request) {
		var err error

		liveReloadWSSConn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			killWebServeCmd()
			killAPIServeCmd()
			logger.Fatal(err)
		}

		for {
			_, _, err := liveReloadWSSConn.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	wss := &http.Server{
		Addr:    server.Config().HTTPHost + ":" + LiveReloadWSSPort,
		Handler: wssHandler,
	}
	wss.ErrorLog = zap.NewStdLog(logger.Desugar())

	go func() {
		if server.Config().HTTPSSLEnabled {
			err := wss.ListenAndServeTLS(server.Config().HTTPSSLCertPath+"/cert.pem", server.Config().HTTPSSLCertPath+"/key.pem")
			if err != http.ErrServerClosed {
				killWebServeCmd()
				killAPIServeCmd()
				logger.Fatal(err)
			}
		}
	}()

	err := ws.ListenAndServe()
	if err != http.ErrServerClosed {
		killWebServeCmd()
		killAPIServeCmd()
		logger.Fatal(err)
	}
}

func watch(logger *Logger, watchPaths []string, callback func(e watcher.Event)) {
	w := watcher.New()
	defer w.Close()

	w.SetMaxEvents(2)

	r := regexp.MustCompile(`.(development|env|go|gql|graphql|ini|json|html|production|test|toml|txt|yml)$`)
	w.AddFilterHook(watcher.RegexFilterHook(r, false))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				killWebServeCmd()
				killAPIServeCmd()
				logger.Fatal(r)
			}
		}()

		for {
			select {
			case event := <-w.Event:
				callback(event)
			case err := <-w.Error:
				killWebServeCmd()
				killAPIServeCmd()
				logger.Fatal(err)
			case <-w.Closed:
				return
			}
		}
	}()

	for _, watchPath := range watchPaths {
		w.AddRecursive(watchPath)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	signal.Notify(quit, syscall.SIGTERM)
	go func() {
		<-quit
		w.Close()
	}()

	if err := w.Start(time.Second * watcherPollInterval); err != nil {
		killWebServeCmd()
		killAPIServeCmd()
		logger.Fatal(err)
	}
}