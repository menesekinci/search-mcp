package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// JSON-RPC 2.0 types used by MCP.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Tool definition for MCP tools/list response.
type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema JSONSchema `json:"inputSchema"`
}

type JSONSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// Server handles MCP requests over stdio.
type Server struct {
	tools   []Tool
	handler ToolHandler
	version string
}

// ToolHandler is called when a tool is invoked.
type ToolHandler func(toolName string, args map[string]any) (string, error)

// NewServer creates an MCP server with the given tools. version is reported
// in the initialize response.
func NewServer(tools []Tool, handler ToolHandler, version string) *Server {
	return &Server{tools: tools, handler: handler, version: version}
}

// Run starts the MCP server reading from stdin and writing to stdout.
func (s *Server) Run() error {
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("mcp: read stdin: %w", err)
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			continue // skip malformed
		}

		resp := s.handle(req)

		out, err := json.Marshal(resp)
		if err != nil {
			continue
		}
		fmt.Fprintf(writer, "%s\n", out)
	}
}

func (s *Server) handle(req Request) Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "notifications/initialized":
		// MCP initialized notification — no response needed
		return Response{}
	default:
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

func (s *Server) handleInitialize(req Request) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]bool{},
			},
			"serverInfo": map[string]string{
				"name":    "search-mcp",
				"version": s.version,
			},
		},
	}
}

func (s *Server) handleToolsList(req Request) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": s.tools,
		},
	}
}

func (s *Server) handleToolsCall(req Request) Response {
	var params struct {
		Name      string         `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	var args map[string]any
	if err := json.Unmarshal(params.Arguments, &args); err != nil {
		args = make(map[string]any)
	}

	result, err := s.handler(params.Name, args)
	if err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": fmt.Sprintf("Error: %s", err.Error())},
				},
				"isError": true,
			},
		}
	}

	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": result},
			},
		},
	}
}
