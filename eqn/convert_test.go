package eqn

import (
	"bytes"
	"testing"
)

func TestConvertBytesRejectsInvalidOLEPayload(t *testing.T) {
	latex, err := ConvertBytes([]byte("not-an-ole"))
	if err == nil {
		t.Fatal("expected invalid OLE payload to return an error")
	}
	if latex != "" {
		t.Fatalf("expected empty latex on invalid payload, got %q", latex)
	}
}

func TestConvertReaderWithContextRejectsInvalidOLEPayloadWithoutPanicking(t *testing.T) {
	reader := bytes.NewReader([]byte("still-not-an-ole"))
	latex, err := ConvertReaderWithContext(reader, "unit-test")
	if err == nil {
		t.Fatal("expected invalid OLE payload to return an error")
	}
	if latex != "" {
		t.Fatalf("expected empty latex on invalid payload, got %q", latex)
	}
}
