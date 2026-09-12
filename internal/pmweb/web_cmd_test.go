package pmweb

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestLyssnaTarNastaLedigaPortUtanFlagga(t *testing.T) {
	upptagen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upptagen.Close()
	_, hamnPort, _ := net.SplitHostPort(upptagen.Addr().String())
	port, _ := strconv.Atoi(hamnPort)

	lyssnare, vald, err := lyssna("127.0.0.1", port, false)
	if err != nil {
		t.Fatalf("PM hittade ingen ledig port: %v", err)
	}
	defer lyssnare.Close()
	if vald == port {
		t.Fatalf("PM tog den upptagna porten %d", port)
	}
	if vald < port || vald > port+webbportSpann {
		t.Fatalf("PM valde en port utanför spannet: %d", vald)
	}
}

func TestLyssnaSagerTillNarValdPortArUpptagen(t *testing.T) {
	upptagen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upptagen.Close()
	_, hamnPort, _ := net.SplitHostPort(upptagen.Addr().String())
	port, _ := strconv.Atoi(hamnPort)

	lyssnare, _, err := lyssna("127.0.0.1", port, true)
	if err == nil {
		lyssnare.Close()
		t.Fatal("PM tog en upptagen port som användaren valt")
	}
	if !strings.Contains(err.Error(), "--port") {
		t.Fatalf("felet säger inte vad användaren ska göra: %v", err)
	}
}
