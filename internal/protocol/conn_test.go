package protocol

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
)

func TestConnReadsAndWritesJSONL(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()
	conn := NewConn(serverSide, 1024)
	defer conn.Close()
	go func() {
		_, _ = clientSide.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"system.health","params":{}}` + "\n"))
	}()
	request, err := conn.ReadRequest()
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != "system.health" {
		t.Fatalf("unexpected method: %s", request.Method)
	}
	go func() { _ = conn.Respond(NewResponse(request.ID, map[string]string{"status": "ok"})) }()
	line, err := bufio.NewReader(clientSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("unexpected error: %+v", response.Error)
	}
}
