package run

import (
	"github.com/ghodss/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"os"
	"testing"
)

func TestGetUnstructuredFromRun(t *testing.T) {
	b, err := os.ReadFile("testdata/run.yaml")
	assert.NoError(t, err)
	t.Logf("%s", string(b))

	run := v1alpha1.Run{}
	err = yaml.Unmarshal(b, &run)
	assert.NoError(t, err)

	r := Reconciler{}
	u, err := r.GetUnstructuredFromRun(&run)
	assert.NoError(t, err)

	gitRevision, _, err := unstructured.NestedString(u.Object, "spec", "source", "git", "revision")
	assert.NoError(t, err)

	t.Logf("gitRevision=%s", gitRevision)

	gitUrl, _, err := unstructured.NestedString(u.Object, "spec", "source", "git", "url")
	assert.NoError(t, err)

	t.Logf("gitUrl=%s", gitUrl)

	assert.Equal(t, "0827e6b753778b17243b188e6a29211457e69d6a", gitRevision)
	assert.Equal(t, "https://github.com/garethjevans/gevans-petclinic", gitUrl)
}
