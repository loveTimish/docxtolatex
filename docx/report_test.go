package docx

import (
	"errors"
	"testing"
)

func TestClassifyOLEError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want EquationReason
	}{
		{
			name: "invalid ole payload",
			err:  errors.New("convert OLE word/embeddings/oleObject1.bin: invalid OLE/MTEF payload (embeddings/oleObject1.bin)"),
			want: EquationReasonInvalidOLE,
		},
		{
			name: "mtef panic",
			err:  errors.New("convert OLE word/embeddings/oleObject1.bin: panic while parsing OLE/MTEF (embeddings/oleObject1.bin): runtime error: invalid memory address or nil pointer dereference"),
			want: EquationReasonMTEFOpenPanic,
		},
		{
			name: "generic convert error",
			err:  errors.New("convert OLE word/embeddings/oleObject1.bin: EOF"),
			want: EquationReasonConvertError,
		},
		{
			name: "nil error",
			err:  nil,
			want: EquationReasonUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyOLEError(tc.err); got != tc.want {
				t.Fatalf("classifyOLEError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestEquationReasonCatalogIncludesCoreReasons(t *testing.T) {
	catalog := EquationReasonCatalog()
	if len(catalog) == 0 {
		t.Fatal("expected non-empty reason catalog")
	}

	seen := map[EquationReason]bool{}
	for _, item := range catalog {
		if item.Reason == EquationReasonUnknown {
			t.Fatal("catalog should not expose the empty unknown reason")
		}
		if item.Category == "" || item.Description == "" {
			t.Fatalf("catalog entry should be fully populated, got %#v", item)
		}
		seen[item.Reason] = true
	}

	for _, want := range []EquationReason{
		EquationReasonConvertError,
		EquationReasonInvalidOLE,
		EquationReasonMTEFOpenPanic,
		EquationReasonEmptyOutput,
		EquationReasonTinyNonLatex,
	} {
		if !seen[want] {
			t.Fatalf("expected reason %q in catalog", want)
		}
	}
}
