package server

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/llmariner/api-usage/pkg/sender"
	v1 "github.com/llmariner/inference-manager/api/v1"
	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/llmariner/inference-manager/server/internal/rate"
	mv1 "github.com/llmariner/model-manager/api/v1"
	vsv1 "github.com/llmariner/vector-store-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateAuditoTranscription(t *testing.T) {
	const modelID = "m0"

	logger := testutil.NewTestLogger(t)
	capturingTaskSender := &captureAudioTranscriptionTaskSender{}
	srv := New(
		&fakeMetricsMonitor{},
		&sender.NoopUsageSetter{},
		rate.NewLimiter(rate.Config{}, logger),
		&fakeModelClient{
			models: map[string]*mv1.Model{
				modelID: {},
			},
		},
		&fakeVectorStoreClient{
			vs: &vsv1.VectorStore{
				Name: "test",
			},
		},
		&fakeRewriter{},
		capturingTaskSender,
		nil,
		logger,
	)
	srv.enableAuth = true

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	fw, err := w.CreateFormFile("file", "test-file.wav")
	assert.NoError(t, err)
	_, err = fw.Write([]byte("hello"))
	assert.NoError(t, err)

	fw, err = w.CreateFormField("model")
	assert.NoError(t, err)
	_, err = fw.Write([]byte(modelID))
	assert.NoError(t, err)

	err = w.Close()
	assert.NoError(t, err)

	req, err := http.NewRequest("POST", "v1/audio/transcriptions", &b)
	assert.NoError(t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rw := &httptest.ResponseRecorder{}
	pathParams := map[string]string{}
	srv.CreateAudioTranscription(rw, req, pathParams)
	assert.Equal(t, http.StatusOK, rw.Code)

	creq := capturingTaskSender.capturedReq
	assert.Equal(t, modelID, creq.Model)
	assert.Equal(t, "test-file.wav", creq.Filename)
	assert.Equal(t, []byte("hello"), creq.File)
}

type captureAudioTranscriptionTaskSender struct {
	capturedReq *v1.CreateAudioTranscriptionRequest
}

func (s *captureAudioTranscriptionTaskSender) SendChatCompletionTask(ctx context.Context, tenantID string, req *v1.CreateChatCompletionRequest, header http.Header) (*http.Response, error) {
	return nil, status.Errorf(codes.Unimplemented, "not implemented")
}

func (s *captureAudioTranscriptionTaskSender) SendEmbeddingTask(ctx context.Context, tenantID string, req *v1.CreateEmbeddingRequest, header http.Header) (*http.Response, error) {
	return nil, status.Errorf(codes.Unimplemented, "not implemented")
}
func (s *captureAudioTranscriptionTaskSender) SendAudioTranscriptionTask(ctx context.Context, tenantID string, req *v1.CreateAudioTranscriptionRequest, header http.Header) (*http.Response, error) {
	s.capturedReq = req
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
}
