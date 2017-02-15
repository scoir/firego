package firego

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zabawaba99/firego/internal/firetest"
)

const URL = "https://somefirebaseapp.firebaseIO.com"

type TestServer struct {
	*httptest.Server
	receivedReqs []*http.Request
}

func newTestServer(response string) *TestServer {
	ts := &TestServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ts.receivedReqs = append(ts.receivedReqs, req)
		fmt.Fprint(w, response)
	}))
	return ts
}

func TestNew(t *testing.T) {
	t.Parallel()
	testURLs := []string{
		URL,
		URL + "/",
		"somefirebaseapp.firebaseIO.com",
		"somefirebaseapp.firebaseIO.com/",
	}

	for _, url := range testURLs {
		fb := New(url, nil)
		assert.Equal(t, URL, fb.(*firebase).url, "givenURL: %s", url)
	}
}

func TestNewWithProvidedHttpClient(t *testing.T) {
	t.Parallel()

	var client = http.DefaultClient
	testURLs := []string{
		URL,
		URL + "/",
		"somefirebaseapp.firebaseIO.com",
		"somefirebaseapp.firebaseIO.com/",
	}

	for _, url := range testURLs {
		fb := New(url, client)
		assert.Equal(t, URL, fb.(*firebase).url, "givenURL: %s", url)
		assert.Equal(t, client, fb.(*firebase).client)
	}
}

func TestAuth(t *testing.T) {
	t.Parallel()
	server := firetest.New()
	server.Start()
	defer server.Close()

	server.RequireAuth(true)
	fb := New(server.URL, nil)

	fb.Auth(server.Secret)
	var v interface{}
	err := fb.Value(&v)
	assert.NoError(t, err)
}

func TestUnauth(t *testing.T) {
	t.Parallel()
	server := firetest.New()
	server.Start()
	defer server.Close()

	server.RequireAuth(true)
	fb := New(server.URL, nil)

	fb.(*firebase).params.Add("auth", server.Secret)
	fb.Unauth()
	err := fb.Value("")
	assert.Error(t, err)
}

func TestExists(t *testing.T) {
	t.Parallel()
	server := firetest.New()
	server.Start()
	defer server.Close()

	fb := New(server.URL, nil)
	// Test Exist on an empty ref
	b, err := fb.Exists()
	require.NoError(t, err)
	assert.False(t, b)

	//Test Exists on an non-empty ref
	payload := map[string]interface{}{"foo": "bar"}
	fb.Push(payload)
	b, err = fb.Exists()
	require.NoError(t, err)
	assert.True(t, b)

}

func TestPush(t *testing.T) {
	t.Parallel()
	var (
		payload = map[string]interface{}{"foo": "bar"}
		server  = firetest.New()
	)
	server.Start()
	defer server.Close()

	fb := New(server.URL, nil)
	childRef, err := fb.Push(payload)
	assert.NoError(t, err)

	path := strings.TrimPrefix(childRef.String(), server.URL+"/")
	v := server.Get(path)
	assert.Equal(t, payload, v)

	childRef.Auth(server.Secret)
	var m map[string]interface{}
	require.NoError(t, childRef.Value(&m))
	assert.Equal(t, payload, m, childRef.String())
}

func TestRemove(t *testing.T) {
	t.Parallel()
	server := firetest.New()
	server.Start()
	defer server.Close()

	server.Set("", true)

	fb := New(server.URL, nil)
	err := fb.Remove()
	assert.NoError(t, err)

	v := server.Get("")
	assert.Nil(t, v)
}

func TestSet(t *testing.T) {
	t.Parallel()
	var (
		payload = map[string]interface{}{"foo": "bar"}
		server  = firetest.New()
	)
	server.Start()
	defer server.Close()

	fb := New(server.URL, nil)
	err := fb.Set(payload)
	assert.NoError(t, err)

	v := server.Get("")
	assert.Equal(t, payload, v)
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	var (
		payload = map[string]interface{}{"foo": "bar"}
		server  = firetest.New()
	)
	server.Start()
	defer server.Close()

	fb := New(server.URL, nil)
	err := fb.Update(payload)
	assert.NoError(t, err)

	v := server.Get("")
	assert.Equal(t, payload, v)
}

func TestValue(t *testing.T) {
	t.Parallel()
	var (
		response = map[string]interface{}{"foo": "bar"}
		server   = firetest.New()
	)
	server.Start()
	defer server.Close()

	fb := New(server.URL, nil)

	server.Set("", response)

	var v map[string]interface{}
	err := fb.Value(&v)
	assert.NoError(t, err)
	assert.Equal(t, response, v)
}

func TestChild(t *testing.T) {
	t.Parallel()
	var (
		parent    = New(URL, nil)
		childNode = "node"
		child     = parent.Child(childNode)
	)

	assert.Equal(t, fmt.Sprintf("%s/%s", parent.(*firebase).url, childNode), child.(*firebase).url)
}

func TestChild_Issue26(t *testing.T) {
	t.Parallel()
	parent := New(URL, nil)
	child1 := parent.Child("one")
	child2 := child1.Child("two")

	child1.Shallow(true)
	assert.Len(t, child2.(*firebase).params, 0)
}

func TestTimeoutDuration_Headers(t *testing.T) {
	var fb Firebase
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(2 * fb.(*firebase).clientTimeout)
		close(done)
	}))
	defer server.Close()

	fb = New(server.URL, nil)
	fb.(*firebase).clientTimeout = time.Millisecond
	err := fb.Value("")
	<-done
	assert.NotNil(t, err)
	assert.IsType(t, ErrTimeout{}, err)

	// ResponseHeaderTimeout should be TimeoutDuration less the time it took to dial, and should be positive
	require.IsType(t, (*http.Transport)(nil), fb.(*firebase).client.Transport)
	tr := fb.(*firebase).client.Transport.(*http.Transport)
	assert.True(t, tr.ResponseHeaderTimeout < TimeoutDuration)
	assert.True(t, tr.ResponseHeaderTimeout > 0)
}

func TestTimeoutDuration_Dial(t *testing.T) {
	fb := New("http://dialtimeouterr.or/", nil)
	fb.(*firebase).clientTimeout = time.Millisecond

	err := fb.Value("")
	assert.NotNil(t, err)
	assert.IsType(t, ErrTimeout{}, err)

	// ResponseHeaderTimeout should be negative since the total duration was consumed when dialing
	require.IsType(t, (*http.Transport)(nil), fb.(*firebase).client.Transport)
	assert.True(t, fb.(*firebase).client.Transport.(*http.Transport).ResponseHeaderTimeout < 0)
}
