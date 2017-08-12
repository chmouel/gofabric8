package cmds

import (
	"testing"

	projectapi "github.com/openshift/origin/pkg/project/apis/project"
)

func TestGetProject(t *testing.T) {
	server, c := fakeTestRestResponder("/", CONFIG_MAP_LIST_JSON)
	defer server.Close()

	p1 := projectapi.Project{}
	p2 := projectapi.Project{}
	p3 := projectapi.Project{}
	p4 := projectapi.Project{}

	p1.Name = "foo-che"
	p2.Name = "bar-che"
	p3.Name = "foo"
	p4.Name = "moto"
	// Pick the first one if when we have multiples projects and we are
	// currently in an unrelated project
	res := detectCurrentUserProject("moto", []projectapi.Project{p1, p2, p3, p4}, c)
	if res != p3.Name {
		t.Fatalf("%s != foo", res)
	}

	p1.Name = "foo-che"
	p2.Name = "bar-che"
	p3.Name = "foo"
	p4.Name = "bar"
	// Return the second project cause we are currently in there
	res = detectCurrentUserProject("bar-che", []projectapi.Project{p1, p2, p3, p4}, c)
	if res != "bar" {
		t.Fatalf("%s != bar", res)
	}

	p1.Name = "foo-che"
	p2.Name = "bar-che"
	p3.Name = "foo"
	p4.Name = "bar"
	// Return bar because we are currently in the namespace bar-jenkins which has the same
	// prefix)
	res = detectCurrentUserProject("bar-jenkins", []projectapi.Project{p1, p2, p3, p4}, c)
	if res != "bar" {
		t.Fatalf("%s != %s", res, "bar")
	}

	// Return an error here, cause we have a foo-che but we don't have a parent
	// project without prefix (i.e: foo)
	p1.Name = "foo-che"
	p2.Name = "moto"
	res = detectCurrentUserProject("moto", []projectapi.Project{p1, p2}, c)
	if res != "" {
		t.Fatalf("%s != foo", res)
	}

	// test if we can get properly the -jenkins and not just the *-che
	p1.Name = "foo"
	p2.Name = "foo-jenkins"
	p3.Name = "hellomoto"
	res = detectCurrentUserProject("hellomoto", []projectapi.Project{p1, p2, p3}, c)
	if res != "foo" {
		t.Fatalf("%s != foo", res)
	}
}
