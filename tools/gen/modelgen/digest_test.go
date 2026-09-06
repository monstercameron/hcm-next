package modelgen

import "testing"

func TestOutputDigestOrderIndependent(t *testing.T) {
	a := map[string][]byte{"a.go": []byte("A"), "b.go": []byte("B")}
	b := map[string][]byte{"b.go": []byte("B"), "a.go": []byte("A")}
	if OutputDigest(a) != OutputDigest(b) {
		t.Fatal("OutputDigest depends on map insertion/iteration order")
	}
}

func TestOutputDigestSensitiveToContent(t *testing.T) {
	a := map[string][]byte{"a.go": []byte("A")}
	b := map[string][]byte{"a.go": []byte("B")}
	if OutputDigest(a) == OutputDigest(b) {
		t.Fatal("OutputDigest did not change when file content changed")
	}
}

func TestOutputDigestSensitiveToFilename(t *testing.T) {
	a := map[string][]byte{"a.go": []byte("A")}
	b := map[string][]byte{"z.go": []byte("A")}
	if OutputDigest(a) == OutputDigest(b) {
		t.Fatal("OutputDigest did not change when the filename changed")
	}
}

func TestGenerateAllProducesOutputFile(t *testing.T) {
	files, err := GenerateAll()
	if err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	if _, ok := files[OutputFile]; !ok {
		t.Fatalf("GenerateAll did not produce %s", OutputFile)
	}
}
