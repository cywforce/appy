package pack

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/appist/appy/support"
	"github.com/appist/appy/test"
)

var (
	testResponse = "Gzip Test Response "
)

type mdwGzipSuite struct {
	test.Suite
	asset  *support.Asset
	config *support.Config
	logger *support.Logger
}

func (s *mdwGzipSuite) SetupTest() {
	os.Setenv("APPY_ENV", "development")
	os.Setenv("APPY_MASTER_KEY", "481e5d98a31585148b8b1dfb6a3c0465")
	os.Setenv("HTTP_CSRF_SECRET", "481e5d98a31585148b8b1dfb6a3c0465")
	os.Setenv("HTTP_SESSION_SECRETS", "481e5d98a31585148b8b1dfb6a3c0465")

	s.logger, _, _ = support.NewTestLogger()
	s.asset = support.NewAsset(nil, "")
	s.config = support.NewConfig(s.asset, s.logger)
}

func (s *mdwGzipSuite) TearDownTest() {
	os.Unsetenv("APPY_ENV")
	os.Unsetenv("APPY_MASTER_KEY")
	os.Unsetenv("HTTP_CSRF_SECRET")
	os.Unsetenv("HTTP_SESSION_SECRETS")
}

func (s *mdwGzipSuite) TestMdwGzip() {
	server := NewServer(s.asset, s.config, s.logger)
	server.Use(mdwGzip(s.config))
	server.GET("/", func(c *Context) {
		c.Header("Content-Length", strconv.Itoa(len(testResponse)))
		c.String(http.StatusOK, testResponse)
	})
	w := server.TestHTTPRequest("GET", "/", H{"Accept-Encoding": "gzip"}, nil)

	s.Equal(http.StatusOK, w.Code)
	s.Equal("gzip", w.Header().Get("Content-Encoding"))
	s.NotEqual("0", w.Header().Get("Content-Length"))
	s.Equal("Accept-Encoding", w.Header().Get("Vary"))
	s.NotEqual(19, w.Body.Len())
	s.Equal(w.Header().Get("Content-Length"), fmt.Sprint(w.Body.Len()))

	gr, err := gzip.NewReader(w.Body)
	s.NoError(err)
	defer gr.Close()

	body, _ := ioutil.ReadAll(gr)
	s.Equal(testResponse, string(body))
}

func (s *mdwGzipSuite) TestGzipWithReverseProxy() {
	req, _ := http.NewRequest("GET", "/reverse", nil)
	req.Header.Add("Accept-Encoding", "gzip")
	w := newCloseNotifyingRecorder()

	server := NewServer(s.asset, s.config, s.logger)
	server.Use(mdwGzip(s.config))
	server.GET("/reverse", func(c *Context) {
		c.Header("Content-Length", strconv.Itoa(len(testResponse)))
		c.String(http.StatusOK, testResponse)
	})
	server.ServeHTTP(w, req)

	s.Equal(http.StatusOK, w.Code)
	s.Equal("gzip", w.Header().Get("Content-Encoding"))
	s.NotEqual("0", w.Header().Get("Content-Length"))
	s.Equal("Accept-Encoding", w.Header().Get("Vary"))
	s.NotEqual(19, w.Body.Len())
	s.Equal(w.Header().Get("Content-Length"), fmt.Sprint(w.Body.Len()))

	gr, err := gzip.NewReader(w.Body)
	s.NoError(err)
	defer gr.Close()

	body, _ := ioutil.ReadAll(gr)
	s.Equal(testResponse, string(body))
}

func (s *mdwGzipSuite) TestNomdwGzip() {
	server := NewServer(s.asset, s.config, s.logger)
	server.GET("/", func(c *Context) {
		c.Header("Content-Length", strconv.Itoa(len(testResponse)))
		c.String(http.StatusOK, testResponse)
	})
	w := server.TestHTTPRequest("GET", "/", nil, nil)

	s.Equal(http.StatusOK, w.Code)
	s.Equal("", w.Header().Get("Content-Encoding"))
	s.Equal("19", w.Header().Get("Content-Length"))
	s.Equal(testResponse, w.Body.String())
}

func (s *mdwGzipSuite) TestUpgradeConnection() {
	server := NewServer(s.asset, s.config, s.logger)
	server.Use(mdwGzip(s.config))
	server.GET("/index.html", func(c *Context) {
		c.String(http.StatusOK, "this is a HTML!")
	})
	w := server.TestHTTPRequest("GET", "/index.html", H{"Content-Type": "text/event-stream"}, nil)

	s.Equal(http.StatusOK, w.Code)
	s.Equal("", w.Header().Get("Content-Encoding"))
	s.Equal("", w.Header().Get("Vary"))
	s.Equal("", w.Header().Get("Content-Length"))
	s.Equal("this is a HTML!", w.Body.String())
}

func (s *mdwGzipSuite) TestExcludedExts() {
	s.config.HTTPGzipExcludedExts = []string{".html"}
	server := NewServer(s.asset, s.config, s.logger)
	server.Use(mdwGzip(s.config))
	server.GET("/index.html", func(c *Context) {
		c.String(http.StatusOK, "this is a HTML!")
	})
	w := server.TestHTTPRequest("GET", "/index.html", H{"Accept-Encoding": "gzip"}, nil)

	s.Equal(http.StatusOK, w.Code)
	s.Equal("", w.Header().Get("Content-Encoding"))
	s.Equal("", w.Header().Get("Content-Length"))
	s.Equal("", w.Header().Get("Vary"))
	s.Equal("this is a HTML!", w.Body.String())
}

func (s *mdwGzipSuite) TestExcludedPaths() {
	s.config.HTTPGzipExcludedPaths = []string{"/api"}
	server := NewServer(s.asset, s.config, s.logger)
	server.Use(mdwGzip(s.config))
	server.GET("/api/books", func(c *Context) {
		c.String(http.StatusOK, "this is a book!")
	})
	w := server.TestHTTPRequest("GET", "/api/books", H{"Accept-Encoding": "gzip"}, nil)

	s.Equal(http.StatusOK, w.Code)
	s.Equal("", w.Header().Get("Content-Encoding"))
	s.Equal("", w.Header().Get("Content-Length"))
	s.Equal("", w.Header().Get("Vary"))
	s.Equal("this is a book!", w.Body.String())
}

func (s *mdwGzipSuite) TestGzipDecompress() {
	buf := &bytes.Buffer{}
	gz, _ := gzip.NewWriterLevel(buf, gzip.DefaultCompression)
	if _, err := gz.Write([]byte(testResponse)); err != nil {
		gz.Close()
		s.FailNow(err.Error())
	}
	gz.Close()

	server := NewServer(s.asset, s.config, s.logger)
	server.Use(mdwGzip(s.config))
	server.POST("/", func(c *Context) {
		if v := c.Request.Header.Get("Content-Encoding"); v != "" {
			s.FailNowf("unexpected `Content-Encoding`: %s header", v)
		}

		if v := c.Request.Header.Get("Content-Length"); v != "" {
			s.FailNowf("unexpected `Content-Length`: %s header", v)
		}

		data, err := c.GetRawData()
		if err != nil {
			s.FailNow(err.Error())
		}

		c.Data(http.StatusOK, "text/plain", data)
	})
	w := server.TestHTTPRequest("POST", "/", H{"Content-Encoding": "gzip"}, buf)

	s.Equal(http.StatusOK, w.Code)
	s.Equal("", w.Header().Get("Content-Encoding"))
	s.Equal("", w.Header().Get("Content-Length"))
	s.Equal("", w.Header().Get("Vary"))
	s.Equal(testResponse, w.Body.String())
}

func (s *mdwGzipSuite) TestGzipDecompressWithEmptyBody() {
	server := NewServer(s.asset, s.config, s.logger)
	server.Use(mdwGzip(s.config))
	server.POST("/", func(c *Context) {
		c.String(http.StatusOK, "ok")
	})
	w := server.TestHTTPRequest("POST", "/", H{"Content-Encoding": "gzip"}, nil)

	s.Equal(http.StatusOK, w.Code)
	s.Equal("", w.Header().Get("Content-Encoding"))
	s.Equal("", w.Header().Get("Content-Length"))
	s.Equal("", w.Header().Get("Vary"))
	s.Equal("ok", w.Body.String())
}

func (s *mdwGzipSuite) TestGzipDecompressWithIncorrectData() {
	server := NewServer(s.asset, s.config, s.logger)
	server.Use(mdwGzip(s.config))
	server.POST("/", func(c *Context) {
		c.String(http.StatusOK, "ok")
	})
	w := server.TestHTTPRequest("POST", "/", H{"Content-Encoding": "gzip"}, bytes.NewReader([]byte(testResponse)))

	s.Equal(http.StatusBadRequest, w.Code)
}

func TestMdwGzipSuite(t *testing.T) {
	test.Run(t, new(mdwGzipSuite))
}

type closeNotifyingRecorder struct {
	*httptest.ResponseRecorder
	closed chan bool
}

func newCloseNotifyingRecorder() *closeNotifyingRecorder {
	return &closeNotifyingRecorder{
		httptest.NewRecorder(),
		make(chan bool, 1),
	}
}

func (c *closeNotifyingRecorder) close() {
	c.closed <- true
}

func (c *closeNotifyingRecorder) CloseNotify() <-chan bool {
	return c.closed
}
