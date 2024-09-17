package roomapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/alex65536/day20/internal/util/httputil"
)

type ClientOptions struct {
	Endpoint string
	Token    string
}

type client struct {
	o      ClientOptions
	client *http.Client
}

func NewClient(o ClientOptions, httpClient *http.Client) API {
	return &client{o: o, client: httpClient}
}

func (c *client) setUpRequest(req *http.Request) {
	req.Header.Add("Authorization", "Bearer "+c.o.Token)
	req.Header.Add("Content-Type", "application/json")
}

func (c *client) decodeError(rsp *http.Response) error {
	if 200 <= rsp.StatusCode && rsp.StatusCode <= 299 {
		return nil
	}
	var b bytes.Buffer
	_, err := io.Copy(&b, rsp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if rsp.Header.Get("Content-Type") == "application/json" {
		var apiErr *Error
		if err := json.Unmarshal(b.Bytes(), &apiErr); err != nil {
			return fmt.Errorf("unmarshal json: %w", err)
		}
		if apiErr.Code == ErrInvalidCode {
			return fmt.Errorf("bad error json")
		}
		return apiErr
	}
	return httputil.MakeError(rsp.StatusCode, b.String())
}

func doClientRequest[Req any, Rsp any](ctx context.Context, c *client, path string, req *Req) (*Rsp, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	hReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.o.Endpoint+path, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setUpRequest(hReq)
	hRsp, err := c.client.Do(hReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, hRsp.Body)
		_ = hRsp.Body.Close()
	}()
	if err := c.decodeError(hRsp); err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	rspBytes, err := io.ReadAll(hRsp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var rsp *Rsp
	if err := json.Unmarshal(rspBytes, &rsp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return rsp, nil
}

func (c *client) Update(ctx context.Context, req *UpdateRequest) (*UpdateResponse, error) {
	return doClientRequest[UpdateRequest, UpdateResponse](ctx, c, "/update", req)
}

func (c *client) Job(ctx context.Context, req *JobRequest) (*JobResponse, error) {
	return doClientRequest[JobRequest, JobResponse](ctx, c, "/job", req)
}

func (c *client) Hello(ctx context.Context, req *HelloRequest) (*HelloResponse, error) {
	return doClientRequest[HelloRequest, HelloResponse](ctx, c, "/hello", req)
}

func (c *client) Bye(ctx context.Context, req *ByeRequest) (*ByeResponse, error) {
	return doClientRequest[ByeRequest, ByeResponse](ctx, c, "/bye", req)
}
