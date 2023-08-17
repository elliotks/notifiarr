package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/Notifiarr/notifiarr/pkg/mnd"
	"github.com/Notifiarr/notifiarr/pkg/website/clientinfo"
	"github.com/gorilla/mux"
	apachelog "github.com/lestrrat-go/apache-logformat/v2"
	mulery "golift.io/mulery/client"
)

// StartWebServer starts the web server.
func (c *Client) StartWebServer(ctx context.Context) {
	c.Lock()
	defer c.Unlock()

	//nolint:lll // Create an apache-style logger.
	apache, _ := apachelog.New(`%{X-Forwarded-For}i - %{X-NotiClient-Username}i %t "%m %{X-Redacted-URI}i %H" %>s %b "%{Referer}i" "%{User-agent}i" %{X-Request-Time}i %{ms}Tms`)

	// Create a request router.
	c.Config.Router = mux.NewRouter()
	c.Config.Router.Use(c.fixForwardedFor)
	c.Config.Router.Use(c.countRequest)
	c.Config.Router.Use(c.addUsernameHeader)
	c.webauth = c.Config.UIPassword.Webauth() // this needs to be locked since password can be changed without reloading.
	c.noauth = c.Config.UIPassword.Noauth()
	c.authHeader = c.Config.UIPassword.Header()

	// Make a multiplexer because websockets can't use apache log.
	smx := http.NewServeMux()
	smx.Handle("/", c.stripSecrets(apache.Wrap(c.Config.Router, c.Logger.HTTPLog.Writer())))
	smx.Handle(path.Join(c.Config.URLBase, "ui", "ws"), c.Config.Router) // websockets cannot go through the apache logger.

	// Create a server.
	c.server = &http.Server{ //nolint: exhaustivestruct
		Handler:           smx,
		Addr:              c.Config.BindAddr,
		IdleTimeout:       time.Minute,
		WriteTimeout:      c.Config.Timeout.Duration,
		ReadTimeout:       c.Config.Timeout.Duration,
		ReadHeaderTimeout: c.Config.Timeout.Duration,
		ErrorLog:          c.Logger.ErrorLog,
	}

	// Start the Notifiarr.com origin websocket tunnel.
	c.startTunnel(ctx)
	// Initialize all the application API paths.
	c.Config.Apps.InitHandlers()
	c.httpHandlers()
	// Run the server.
	go c.runWebServer()
}

func (c *Client) startTunnel(ctx context.Context) {
	// If clientinfo is nil, then we probably have a bad API key.
	ci := clientinfo.Get()
	if ci == nil {
		return
	}

	const (
		maxPoolSize = 25 // maximum websocket connections to the origin (mulery server).
		maxPoolMin  = 4  // maximum is calculated, and this is the minimum it may be.
	)

	poolmax := len(c.Config.Apps.Sonarr) + len(c.Config.Apps.Radarr) + len(c.Config.Apps.Lidarr) +
		len(c.Config.Apps.Readarr) + len(c.Config.Apps.Prowlarr) + len(c.Config.Apps.Deluge) +
		len(c.Config.Apps.Qbit) + len(c.Config.Apps.Rtorrent) + len(c.Config.Apps.SabNZB) +
		len(c.Config.Apps.NZBGet) + 1

	if c.Config.Apps.Plex.Enabled() {
		poolmax++
	}

	if c.Config.Apps.Tautulli.Enabled() {
		poolmax++
	}

	if poolmax > maxPoolSize {
		poolmax = maxPoolSize
	} else if poolmax < maxPoolMin {
		poolmax = maxPoolMin
	}

	hostname, _ := os.Hostname()
	if hostInfo, err := c.clientinfo.GetHostInfo(ctx); err != nil {
		hostname = hostInfo.Hostname
	}

	// This apache logger is only used for client->server websocket-tunneled requests.
	remWs, _ := apachelog.New(`%{X-Forwarded-For}i %{X-User-ID}i env:%{X-User-Environment}i %t "%r" %>s %b ` +
		`"%{X-Client-ID}i" "%{User-agent}i" %{X-Request-Time}i %{ms}Tms`)
	//nolint:gomnd // just attempting a tiny bit of splay.
	c.tunnel = mulery.NewClient(&mulery.Config{
		Name:          hostname,
		ID:            c.Config.HostID,
		ClientIDs:     []any{ci.User.ID},
		Targets:       ci.User.Tunnels,
		PoolIdleSize:  1,
		PoolMaxSize:   poolmax,
		CleanInterval: time.Second + time.Duration(c.triggers.Timers.Rand().Intn(1000))*time.Millisecond,
		Backoff:       600*time.Millisecond + time.Duration(c.triggers.Timers.Rand().Intn(600))*time.Millisecond,
		SecretKey:     c.Config.APIKey,
		Handler:       remWs.Wrap(c.prefixURLbase(c.Config.Router), c.Logger.HTTPLog.Writer()).ServeHTTP,
		// Logger:        &tunnelLogger{Logger: c.Logger},
		Logger: c.Logger,
	})
	c.tunnel.Start(ctx)
}

// prefixURLbase adds a prefix to an http request.
// We need this to fix websocket-tunneled requests
// from the website when url base is not the default.
func (c *Client) prefixURLbase(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.Config.URLBase == "" || c.Config.URLBase == "/" {
			h.ServeHTTP(w, r)
			return
		}

		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = path.Join(c.Config.URLBase, r.URL.Path)

		if r.URL.RawPath != "" {
			r2.URL.RawPath = path.Join(c.Config.URLBase, r.URL.RawPath)
		}

		h.ServeHTTP(w, r2)
	})
}

// runWebServer starts the http or https listener.
func (c *Client) runWebServer() {
	defer c.CapturePanic()

	var err error

	if menu["stat"] != nil {
		menu["stat"].Check()
		menu["stat"].SetTooltip("web server running, uncheck to pause")
	}

	if c.Config.SSLCrtFile != "" && c.Config.SSLKeyFile != "" {
		err = c.server.ListenAndServeTLS(c.Config.SSLCrtFile, c.Config.SSLKeyFile)
	} else {
		err = c.server.ListenAndServe()
	}

	if err != nil && !errors.Is(http.ErrServerClosed, err) {
		c.Errorf("Web Server Failed: %v (shutting down)", err)
		c.sigkil <- os.Kill // stop the app.
	}
}

// StopWebServer stops the web servers. Panics if that causes an error or timeout.
func (c *Client) StopWebServer(ctx context.Context) error {
	c.Print("==> Stopping Web Server!")

	ctx, cancel := context.WithTimeout(ctx, c.Config.Timeout.Duration)
	defer cancel()

	if menu["stat"] != nil {
		menu["stat"].Uncheck()
		menu["stat"].SetTooltip("web server paused, click to start")
	}

	if c.tunnel != nil {
		defer c.tunnel.Shutdown()
	}

	if err := c.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down web server: %w", err)
	}

	return nil
}

/* Wrap all incoming http calls, so we can stuff counters into expvar. */

var (
	_ = http.ResponseWriter(&responseWrapper{})
	_ = net.Conn(&netConnWrapper{})
)

type responseWrapper struct {
	http.ResponseWriter
	statusCode int
}

type netConnWrapper struct {
	net.Conn
}

func (r *responseWrapper) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseWrapper) Write(b []byte) (int, error) {
	mnd.HTTPRequests.Add("Response Bytes", int64(len(b)))
	return r.ResponseWriter.Write(b) //nolint:wrapcheck
}

func (r *responseWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijack, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		// This fires if you move the /ui/ws endpoint to another name.
		// It needs to be updated in two places.
		panic("cannot hijack connection!")
	}

	conn, buf, err := hijack.Hijack()
	if err != nil {
		return conn, buf, err //nolint:wrapcheck
	}

	return &netConnWrapper{conn}, buf, nil
}

func (n *netConnWrapper) Write(b []byte) (int, error) {
	mnd.HTTPRequests.Add("Response Bytes", int64(len(b)))
	return n.Conn.Write(b) //nolint:wrapcheck
}

// // tunnelLogger lets us tune the logs from the mulery tunnel.
// type tunnelLogger struct {
// 	mnd.Logger
// }

// // Debugf prints a message with DEBUG prefixed.
// func (l *tunnelLogger) Debugf(format string, v ...interface{}) {
// 	l.Logger.Debugf(format, v...)
// }

// // Errorf prints a message with ERROR prefixed.
// func (l *tunnelLogger) Errorf(format string, v ...interface{}) {
// 	l.Logger.ErrorfNoShare(format, v...) // this is why we dont just pass the interface in as-is.
// }

// // Printf prints a message with INFO prefixed.
// func (l *tunnelLogger) Printf(format string, v ...interface{}) {
// 	l.Logger.Printf(format, v...)
// }
