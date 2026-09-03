package mappingprofile

import "testing"

func TestTodo_INTG_006(t *testing.T) {
	p := Profile{MappingID: "pilot", Version: "v1", Rules: []Rule{
		{Source: "name", Target: "person.name", Op: "TRIM"},
		{Source: "dept", Target: "department.id", Op: "ENUM", Lookup: map[string]string{"eng": "ENG-1"}},
		{Source: "optional", Target: "optional", Op: "IDENTITY", Null: NullOmit},
	}}
	c, err := Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.Execute(map[string]string{"name": " Ada ", "dept": "eng"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Fields) != 2 || a.Fields[0].Target != "department.id" || a.Fields[1].Value != "Ada" {
		t.Fatalf("fields: %#v", a.Fields)
	}
	b, err := c.Execute(map[string]string{"dept": "eng", "name": " Ada "})
	if err != nil || a.Digest != b.Digest {
		t.Fatalf("replay digest changed: %v %v", err, b.Digest)
	}
}

func TestTodo_INTG_006_Fault(t *testing.T) {
	if _, err := Compile(Profile{MappingID: "x", Version: "v1", Rules: []Rule{{Source: "x", Target: "y", Op: "EVAL"}}}); err == nil {
		t.Fatal("arbitrary operation accepted")
	}
	if _, err := Compile(Profile{MappingID: "x", Version: "v1", Rules: []Rule{{Source: "x", Target: "y", Op: "DATE", Argument: "2006-01-02 15:04"}}}); err == nil {
		t.Fatal("unbounded date layout accepted")
	}
}

func TestTodo_INTG_006_Golden(t *testing.T) {
	p := Profile{MappingID: "x", Version: "v1", Rules: []Rule{{Source: "x", Target: "y", Op: "TRIM"}}}
	a, _ := Compile(p)
	b, _ := Compile(p)
	if a.Digest() == "" || a.Digest() != b.Digest() {
		t.Fatalf("unstable profile digest")
	}
}
