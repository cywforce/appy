package middleware

import (
	"{{.projectName}}/pkg/app"
	"bufio"
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/appist/appy"
)

type ExampleSuite struct {
	appy.TestSuite
	buffer   *bytes.Buffer
	logger   *appy.Logger
	recorder *httptest.ResponseRecorder
	writer   *bufio.Writer
}

func (s *ExampleSuite) SetupTest() {
	s.logger, s.buffer, s.writer = appy.NewFakeLogger()
	s.recorder = httptest.NewRecorder()
}

func (s *ExampleSuite) TearDownTest() {
}

func (s *ExampleSuite) TestExample() {
	oldLogger := app.Logger
	app.Logger = s.logger
	defer func() { app.Logger = oldLogger }()

	ctx := context.Background()
	job := appy.NewJob("test", nil)

	mockedHandler := appy.NewFakeWorkerHandler()
	mockedHandler.On("ProcessTask", ctx, job).Return(nil)
	err := Example(mockedHandler).ProcessTask(ctx, job)
	s.writer.Flush()

	s.Nil(err)
	s.Contains(s.buffer.String(), "middleware example logging")
	mockedHandler.AssertExpectations(s.T())
}

func TestExampleSuite(t *testing.T) {
	appy.RunTestSuite(t, new(ExampleSuite))
}