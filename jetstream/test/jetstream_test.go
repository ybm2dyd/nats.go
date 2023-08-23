// Copyright 2022-2023 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ybm2dyd/nats.go"
	"github.com/ybm2dyd/nats.go/jetstream"
)

func TestNewWithAPIPrefix(t *testing.T) {
	t.Run("import subject from another account", func(t *testing.T) {
		conf := createConfFile(t, []byte(`
		listen: 127.0.0.1:-1
		no_auth_user: test_user
		jetstream: {max_mem_store: 64GB, max_file_store: 10TB}
		accounts: {
			JS: {
				jetstream: enabled
				users: [ {user: main, password: foo} ]
				exports [ { service: "$JS.API.>" },  { service: "foo" }]
			},
			U: {
				users: [ {user: test_user, password: bar} ]
				imports [
					{ service: { subject: "$JS.API.>", account: JS } , to: "main.>" }
					{ service: { subject: "foo", account: JS } }
				]
			},
		}
		`))
		defer os.Remove(conf)
		srv, _ := RunServerWithConfig(conf)
		defer shutdownJSServerAndRemoveStorage(t, srv)
		ncMain, err := nats.Connect(srv.ClientURL(), nats.UserInfo("main", "foo"))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer ncMain.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		jsMain, err := jetstream.New(ncMain)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = jsMain.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "TEST",
			Subjects: []string{"foo"},
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ncTest, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer ncTest.Close()

		jsTest, err := jetstream.NewWithAPIPrefix(ncTest, "main")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = jsTest.Publish(ctx, "foo", []byte("msg"))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	})

	t.Run("empty API prefix", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		_, err = jetstream.NewWithAPIPrefix(nc, "")
		if err == nil || err.Error() != "API prefix cannot be empty" {
			t.Fatalf(`Expected error: "API prefix cannot be empty"; got: %v`, err)
		}
	})
}

func TestNewWithDomain(t *testing.T) {
	t.Run("jetstream account with domain", func(t *testing.T) {
		conf := createConfFile(t, []byte(`
			listen: 127.0.0.1:-1
			jetstream: { domain: ABC }
		`))
		defer os.Remove(conf)
		srv, _ := RunServerWithConfig(conf)
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo("main", "foo"))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		js, err := jetstream.NewWithDomain(nc, "ABC")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		accInfo, err := js.AccountInfo(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if accInfo.Domain != "ABC" {
			t.Errorf("Invalid domain; want %v, got: %v", "ABC", accInfo.Domain)
		}

		_, err = js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "TEST",
			Subjects: []string{"foo"},
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = js.Publish(ctx, "foo", []byte("msg"))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	})

	t.Run("empty domain", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		_, err = jetstream.NewWithDomain(nc, "")
		if err == nil || err.Error() != "domain cannot be empty" {
			t.Fatalf(`Expected error: "domain cannot be empty"; got: %v`, err)
		}
	})
}

func TestWithClientTrace(t *testing.T) {
	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var sent, received string
	js, err := jetstream.New(nc, jetstream.WithClientTrace(&jetstream.ClientTrace{
		RequestSent: func(subj string, _ []byte) {
			sent = fmt.Sprintf("Request sent: %s", subj)
		},
		ResponseReceived: func(subj string, _ []byte, _ nats.Header) {
			received = fmt.Sprintf("Response received: %s", subj)
		},
	}))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	_, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: "foo", Subjects: []string{"FOO.123"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if sent != "Request sent: $JS.API.STREAM.CREATE.foo" {
		t.Fatalf(`Invalid value on sent request trace; want: "Request sent: $JS.API.STREAM.CREATE.foo"; got: %s`, sent)
	}
	if received != "Response received: $JS.API.STREAM.CREATE.foo" {
		t.Fatalf(`Invalid value on response receive trace; want: "Response received: $JS.API.STREAM.CREATE.foo"; got: %s`, sent)
	}
	defer nc.Close()
}

func TestCreateStream(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		subject   string
		timeout   time.Duration
		withError error
	}{
		{
			name:    "create stream, ok",
			stream:  "foo",
			timeout: 10 * time.Second,
			subject: "FOO.123",
		},
		{
			name:    "with empty context",
			stream:  "foo",
			subject: "FOO.123",
		},
		{
			name:      "invalid stream name",
			stream:    "foo.123",
			subject:   "FOO.123",
			timeout:   10 * time.Second,
			withError: jetstream.ErrInvalidStreamName,
		},
		{
			name:      "stream name required",
			stream:    "",
			subject:   "FOO.123",
			timeout:   10 * time.Second,
			withError: jetstream.ErrStreamNameRequired,
		},
		{
			name:      "stream name already in use",
			stream:    "foo",
			subject:   "BAR.123",
			timeout:   10 * time.Second,
			withError: jetstream.ErrStreamNameAlreadyInUse,
		},
		{
			name:      "context timeout",
			stream:    "foo",
			subject:   "BAR.123",
			timeout:   1 * time.Microsecond,
			withError: context.DeadlineExceeded,
		},
	}

	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), test.timeout)
				defer cancel()
			}
			_, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: test.stream, Subjects: []string{test.subject}})
			if test.withError != nil {
				if !errors.Is(err, test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestCreateStreamMirrorCrossDomains(t *testing.T) {
	test := []struct {
		name         string
		streamConfig *jetstream.StreamConfig
	}{
		{
			name: "create stream mirror cross domains",
			streamConfig: &jetstream.StreamConfig{
				Name: "MIRROR",
				Mirror: &jetstream.StreamSource{
					Name:   "TEST",
					Domain: "HUB",
				},
			},
		},
		{
			name: "create stream with source cross domains",
			streamConfig: &jetstream.StreamConfig{
				Name: "MIRROR",
				Sources: []*jetstream.StreamSource{
					{
						Name:   "TEST",
						Domain: "HUB",
					},
				},
			},
		},
	}

	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			conf := createConfFile(t, []byte(`
		server_name: HUB
		listen: 127.0.0.1:-1
		jetstream: { domain: HUB }
		leafnodes { listen: 127.0.0.1:7422 }
	}`))
			defer os.Remove(conf)
			srv, _ := RunServerWithConfig(conf)
			defer shutdownJSServerAndRemoveStorage(t, srv)

			lconf := createConfFile(t, []byte(`
	server_name: LEAF
	listen: 127.0.0.1:-1
	 jetstream: { domain:LEAF }
	 leafnodes {
		  remotes = [ { url: "leaf://127.0.0.1" } ]
	 }
}`))
			defer os.Remove(lconf)
			ln, _ := RunServerWithConfig(lconf)
			defer shutdownJSServerAndRemoveStorage(t, ln)

			nc, err := nats.Connect(srv.ClientURL())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer nc.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			js, err := jetstream.New(nc)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			_, err = js.CreateStream(ctx, jetstream.StreamConfig{
				Name:     "TEST",
				Subjects: []string{"foo"},
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if _, err := js.Publish(ctx, "foo", []byte("msg1")); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if _, err := js.Publish(ctx, "foo", []byte("msg2")); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			lnc, err := nats.Connect(ln.ClientURL())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer lnc.Close()
			ljs, err := jetstream.New(lnc)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			ccfg := *test.streamConfig
			_, err = ljs.CreateStream(ctx, ccfg)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if !reflect.DeepEqual(test.streamConfig, &ccfg) {
				t.Fatalf("Did not expect config to be altered: %+v vs %+v", test.streamConfig, ccfg)
			}

			// Make sure we sync.
			checkFor(t, 2*time.Second, 15*time.Millisecond, func() error {
				lStream, err := ljs.Stream(ctx, "MIRROR")
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if lStream.CachedInfo().State.Msgs == 2 {
					return nil
				}
				return fmt.Errorf("Did not get synced messages: %d", lStream.CachedInfo().State.Msgs)
			})
			if _, err := ljs.Publish(ctx, "foo", []byte("msg3")); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			lStream, err := ljs.Stream(ctx, "MIRROR")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if lStream.CachedInfo().State.Msgs != 3 {
				t.Fatalf("Expected 3 msgs in stream; got: %d", lStream.CachedInfo().State.Msgs)
			}

			rjs, err := jetstream.NewWithDomain(lnc, "HUB")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			_, err = rjs.Stream(ctx, "TEST")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if _, err := rjs.Publish(ctx, "foo", []byte("msg4")); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			rStream, err := rjs.Stream(ctx, "TEST")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if rStream.CachedInfo().State.Msgs != 4 {
				t.Fatalf("Expected 3 msgs in stream; got: %d", rStream.CachedInfo().State.Msgs)
			}
		})
	}
}

func TestUpdateStream(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		subject   string
		timeout   time.Duration
		withError error
	}{
		{
			name:    "update existing stream",
			stream:  "foo",
			subject: "BAR.123",
			timeout: 10 * time.Second,
		},
		{
			name:    "with empty context",
			stream:  "foo",
			subject: "FOO.123",
		},
		{
			name:      "invalid stream name",
			stream:    "foo.123",
			subject:   "FOO.123",
			timeout:   10 * time.Second,
			withError: jetstream.ErrInvalidStreamName,
		},
		{
			name:      "stream name required",
			stream:    "",
			subject:   "FOO.123",
			timeout:   10 * time.Second,
			withError: jetstream.ErrStreamNameRequired,
		},
		{
			name:      "stream not found",
			stream:    "bar",
			subject:   "FOO.123",
			timeout:   10 * time.Second,
			withError: jetstream.ErrStreamNotFound,
		},
		{
			name:      "context timeout",
			stream:    "foo",
			subject:   "FOO.123",
			timeout:   1 * time.Microsecond,
			withError: context.DeadlineExceeded,
		},
	}

	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()
	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "foo", Subjects: []string{"FOO.123"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), test.timeout)
				defer cancel()
			}
			s, err := js.UpdateStream(ctx, jetstream.StreamConfig{Name: test.stream, Subjects: []string{test.subject}})
			if test.withError != nil {
				if !errors.Is(err, test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			info, err := s.Info(ctx)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if len(info.Config.Subjects) != 1 || info.Config.Subjects[0] != test.subject {
				t.Fatalf("Invalid stream subjects after update: %v", info.Config.Subjects)
			}
		})
	}
}

func TestStream(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		subject   string
		timeout   time.Duration
		withError error
	}{
		{
			name:    "get existing stream",
			stream:  "foo",
			timeout: 10 * time.Second,
		},
		{
			name:   "with empty context",
			stream: "foo",
		},
		{
			name:      "invalid stream name",
			stream:    "foo.123",
			timeout:   10 * time.Second,
			withError: jetstream.ErrInvalidStreamName,
		},
		{
			name:      "stream name required",
			stream:    "",
			timeout:   10 * time.Second,
			withError: jetstream.ErrStreamNameRequired,
		},
		{
			name:      "stream not found",
			stream:    "bar",
			timeout:   10 * time.Second,
			withError: jetstream.ErrStreamNotFound,
		},
		{
			name:      "context timeout",
			stream:    "foo",
			timeout:   1 * time.Microsecond,
			withError: context.DeadlineExceeded,
		},
	}
	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()
	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "foo", Subjects: []string{"FOO.123"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), test.timeout)
				defer cancel()
			}
			s, err := js.Stream(ctx, test.stream)
			if test.withError != nil {
				if !errors.Is(err, test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if s.CachedInfo().Config.Name != test.stream {
				t.Fatalf("Invalid stream fetched; want: foo; got: %s", s.CachedInfo().Config.Name)
			}
		})
	}
}

func TestDeleteStream(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		subject   string
		timeout   time.Duration
		withError error
	}{
		{
			name:    "delete existing stream",
			stream:  "foo",
			timeout: 10 * time.Second,
		},
		{
			name:   "with empty context",
			stream: "bar",
		},
		{
			name:      "invalid stream name",
			stream:    "foo.123",
			timeout:   10 * time.Second,
			withError: jetstream.ErrInvalidStreamName,
		},
		{
			name:      "stream name required",
			stream:    "",
			timeout:   10 * time.Second,
			withError: jetstream.ErrStreamNameRequired,
		},
		{
			name:      "stream not found",
			stream:    "foo",
			timeout:   10 * time.Second,
			withError: jetstream.ErrStreamNotFound,
		},
	}
	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()
	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "foo", Subjects: []string{"FOO.123"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()
	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "bar", Subjects: []string{"BAR.123"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
			}
			err := js.DeleteStream(ctx, test.stream)
			if test.withError != nil {
				if !errors.Is(err, test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestAccountInfo(t *testing.T) {
	t.Run("fetch account info", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		js, err := jetstream.New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()
		_, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: "foo", Subjects: []string{"FOO.123"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		info, err := js.AccountInfo(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if info.Streams != 1 {
			t.Fatalf("Invalid number of streams; want: 1; got: %d", info.Streams)
		}
	})

	t.Run("jetstream not enabled on server", func(t *testing.T) {
		srv := RunDefaultServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		js, err := jetstream.New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		_, err = js.AccountInfo(ctx)
		if err == nil || !errors.Is(err, jetstream.ErrJetStreamNotEnabled) {
			t.Fatalf(": %v; got: %v", jetstream.ErrJetStreamNotEnabled, err)
		}
	})

	t.Run("jetstream not enabled for account", func(t *testing.T) {
		conf := createConfFile(t, []byte(`
		listen: 127.0.0.1:-1
		jetstream: enabled
		no_auth_user: foo
		accounts: {
			JS: {
				jetstream: disabled
				users: [ {user: foo, password: bar} ]
			},
		}
	`))
		defer os.Remove(conf)
		srv, _ := RunServerWithConfig(conf)
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		js, err := jetstream.New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()
		_, err = js.AccountInfo(ctx)
		if err == nil || !errors.Is(err, jetstream.ErrJetStreamNotEnabledForAccount) {
			t.Fatalf(": %v; got: %v", jetstream.ErrJetStreamNotEnabledForAccount, err)
		}
	})
}

func TestListStreams(t *testing.T) {
	tests := []struct {
		name       string
		streamsNum int
		timeout    time.Duration
		subject    string
		expected   int
		withError  error
	}{
		{
			name:       "list streams",
			streamsNum: 260,
			timeout:    10 * time.Second,
			expected:   260,
		},
		{
			name:       "with empty context",
			streamsNum: 260,
			expected:   260,
		},
		{
			name:       "no stream available",
			timeout:    10 * time.Second,
			streamsNum: 0,
			expected:   0,
		},
		{
			name:       "list streams with subject filter",
			subject:    "FOO.123",
			streamsNum: 260,
			expected:   1,
		},
		{
			name:       "list streams with subject filter, no match",
			subject:    "FOO.500",
			streamsNum: 100,
			expected:   0,
		},
		{
			name:       "context timeout",
			streamsNum: 260,
			timeout:    1 * time.Microsecond,
			withError:  context.DeadlineExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := RunBasicJetStreamServer()
			defer shutdownJSServerAndRemoveStorage(t, srv)
			nc, err := nats.Connect(srv.ClientURL())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), test.timeout)
				defer cancel()
			}
			js, err := jetstream.New(nc)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer nc.Close()
			for i := 0; i < test.streamsNum; i++ {
				_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: fmt.Sprintf("foo%d", i), Subjects: []string{fmt.Sprintf("FOO.%d", i)}})
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			}
			opts := []jetstream.StreamListOpt{}
			if test.subject != "" {
				opts = append(opts, jetstream.WithStreamListSubject(test.subject))
			}
			streamsList := js.ListStreams(ctx, opts...)
			streams := make([]*jetstream.StreamInfo, 0)
			for si := range streamsList.Info() {
				streams = append(streams, si)
			}
			if test.withError != nil {
				if !errors.Is(streamsList.Err(), test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, streamsList.Err())
				}
				return
			}
			if streamsList.Err() != nil {
				t.Fatalf("Unexpected error: %s", streamsList.Err())
			}
			if len(streams) != test.expected {
				t.Fatalf("Wrong number of streams; want: %d; got: %d", test.streamsNum, len(streams))
			}
		})
	}
}

func TestStreamNames(t *testing.T) {
	tests := []struct {
		name       string
		streamsNum int
		subject    string
		expected   int
		timeout    time.Duration
		withError  error
	}{
		{
			name:       "list streams",
			streamsNum: 500,
			timeout:    10 * time.Second,
			expected:   500,
		},
		{
			name:       "with empty context",
			streamsNum: 500,
			expected:   500,
		},
		{
			name:       "no stream available",
			streamsNum: 0,
			expected:   0,
			timeout:    10 * time.Second,
		},
		{
			name:       "list streams with subject filter",
			subject:    "FOO.123",
			streamsNum: 260,
			expected:   1,
		},
		{
			name:       "list streams with subject filter, no match",
			subject:    "FOO.500",
			streamsNum: 100,
			expected:   0,
			timeout:    10 * time.Second,
		},
		{
			name:       "context timeout",
			streamsNum: 500,
			timeout:    1 * time.Microsecond,
			withError:  context.DeadlineExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := RunBasicJetStreamServer()
			defer shutdownJSServerAndRemoveStorage(t, srv)
			nc, err := nats.Connect(srv.ClientURL())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, test.timeout)
				defer cancel()
			}
			js, err := jetstream.New(nc)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer nc.Close()
			for i := 0; i < test.streamsNum; i++ {
				_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: fmt.Sprintf("foo%d", i), Subjects: []string{fmt.Sprintf("FOO.%d", i)}})
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			}
			opts := []jetstream.StreamListOpt{}
			if test.subject != "" {
				opts = append(opts, jetstream.WithStreamListSubject(test.subject))
			}
			streamsList := js.StreamNames(ctx, opts...)
			streams := make([]string, 0)
			for s := range streamsList.Name() {
				streams = append(streams, s)
			}
			if test.withError != nil {
				if !errors.Is(streamsList.Err(), test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, streamsList.Err())
				}
				return
			}
			if streamsList.Err() != nil {
				t.Fatalf("Unexpected error: %s", streamsList.Err())
			}
			if len(streams) != test.expected {
				t.Fatalf("Wrong number of streams; want: %d; got: %d", test.streamsNum, len(streams))
			}
		})
	}
}

func TestJetStream_CreateOrUpdateConsumer(t *testing.T) {
	tests := []struct {
		name           string
		stream         string
		consumerConfig jetstream.ConsumerConfig
		shouldCreate   bool
		timeout        time.Duration
		withError      error
	}{
		{
			name:           "create durable pull consumer",
			stream:         "foo",
			consumerConfig: jetstream.ConsumerConfig{Durable: "dur", AckPolicy: jetstream.AckExplicitPolicy},
			timeout:        10 * time.Second,
			shouldCreate:   true,
		},
		{
			name:           "create ephemeral pull consumer",
			stream:         "foo",
			consumerConfig: jetstream.ConsumerConfig{AckPolicy: jetstream.AckExplicitPolicy},
			timeout:        10 * time.Second,
			shouldCreate:   true,
		},
		{
			name:           "with empty context",
			consumerConfig: jetstream.ConsumerConfig{AckPolicy: jetstream.AckExplicitPolicy},
			stream:         "foo",
			shouldCreate:   true,
		},
		{
			name:           "consumer already exists, update",
			stream:         "foo",
			consumerConfig: jetstream.ConsumerConfig{Durable: "dur", AckPolicy: jetstream.AckExplicitPolicy, Description: "test consumer"},
		},
		{
			name:           "consumer already exists, illegal update",
			stream:         "foo",
			consumerConfig: jetstream.ConsumerConfig{Durable: "dur", AckPolicy: jetstream.AckNonePolicy, Description: "test consumer"},
			withError:      jetstream.ErrConsumerCreate,
		},
		{
			name:      "stream does not exist",
			stream:    "abc",
			withError: jetstream.ErrStreamNotFound,
		},
		{
			name:      "invalid stream name",
			stream:    "foo.1",
			withError: jetstream.ErrInvalidStreamName,
		},
		{
			name:           "invalid durable name",
			stream:         "foo",
			consumerConfig: jetstream.ConsumerConfig{Durable: "dur.123", AckPolicy: jetstream.AckExplicitPolicy},
			withError:      jetstream.ErrInvalidConsumerName,
		},
		{
			name:           "context timeout",
			consumerConfig: jetstream.ConsumerConfig{AckPolicy: jetstream.AckExplicitPolicy},
			stream:         "foo",
			timeout:        1 * time.Microsecond,
			withError:      context.DeadlineExceeded,
		},
	}

	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()

	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), test.timeout)
				defer cancel()
			}
			var sub *nats.Subscription
			if test.consumerConfig.FilterSubject != "" {
				sub, err = nc.SubscribeSync(fmt.Sprintf("$JS.API.CONSUMER.CREATE.foo.*.%s", test.consumerConfig.FilterSubject))
			} else {
				sub, err = nc.SubscribeSync("$JS.API.CONSUMER.CREATE.foo.*")
			}
			c, err := js.CreateOrUpdateConsumer(ctx, test.stream, test.consumerConfig)
			if test.withError != nil {
				if err == nil || !errors.Is(err, test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if test.shouldCreate {
				if _, err := sub.NextMsgWithContext(ctx); err != nil {
					t.Fatalf("Expected request on %s; got %s", sub.Subject, err)
				}
			}
			_, err = js.Consumer(ctx, test.stream, c.CachedInfo().Name)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestJetStream_Consumer(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		durable   string
		timeout   time.Duration
		withError error
	}{
		{
			name:    "get existing consumer",
			stream:  "foo",
			durable: "dur",
			timeout: 10 * time.Second,
		},
		{
			name:    "with empty context",
			stream:  "foo",
			durable: "dur",
		},
		{
			name:      "consumer does not exist",
			stream:    "foo",
			durable:   "abc",
			timeout:   10 * time.Second,
			withError: jetstream.ErrConsumerNotFound,
		},
		{
			name:      "invalid durable name",
			stream:    "foo",
			durable:   "dur.123",
			withError: jetstream.ErrInvalidConsumerName,
		},
		{
			name:      "stream does not exist",
			stream:    "abc",
			durable:   "dur",
			withError: jetstream.ErrStreamNotFound,
		},
		{
			name:      "invalid stream name",
			stream:    "foo.1",
			durable:   "dur",
			withError: jetstream.ErrInvalidStreamName,
		},
		{
			name:      "context timeout",
			stream:    "foo",
			durable:   "dur",
			timeout:   1 * time.Microsecond,
			withError: context.DeadlineExceeded,
		},
	}

	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()

	s, err := js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	_, err = s.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{Durable: "dur", AckPolicy: jetstream.AckAllPolicy, Description: "desc"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), test.timeout)
				defer cancel()
			}
			c, err := js.Consumer(ctx, test.stream, test.durable)
			if test.withError != nil {
				if err == nil || !errors.Is(err, test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if c.CachedInfo().Name != test.durable {
				t.Fatalf("Unexpected consumer fetched; want: %s; got: %s", test.durable, c.CachedInfo().Name)
			}
		})
	}
}

func TestJetStream_DeleteConsumer(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		durable   string
		timeout   time.Duration
		withError error
	}{
		{
			name:    "delete existing consumer",
			stream:  "foo",
			durable: "dur",
			timeout: 10 * time.Second,
		},
		{
			name:    "with empty context",
			stream:  "foo",
			durable: "dur2",
		},
		{
			name:      "consumer does not exist",
			stream:    "foo",
			durable:   "dur",
			timeout:   10 * time.Second,
			withError: jetstream.ErrConsumerNotFound,
		},
		{
			name:      "invalid durable name",
			stream:    "foo",
			durable:   "dur.123",
			withError: jetstream.ErrInvalidConsumerName,
		},
		{
			name:      "stream not found",
			stream:    "abc",
			durable:   "dur",
			withError: jetstream.ErrStreamNotFound,
		},
		{
			name:      "invalid stream name",
			stream:    "foo.1",
			durable:   "dur",
			withError: jetstream.ErrInvalidStreamName,
		},
		{
			name:      "context timeout",
			stream:    "foo",
			durable:   "dur",
			timeout:   1 * time.Microsecond,
			withError: context.DeadlineExceeded,
		},
	}

	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()

	s, err := js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	_, err = s.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{Durable: "dur", AckPolicy: jetstream.AckAllPolicy, Description: "desc"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	_, err = s.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{Durable: "dur2", AckPolicy: jetstream.AckAllPolicy, Description: "desc"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), test.timeout)
				defer cancel()
			}
			err := js.DeleteConsumer(ctx, test.stream, test.durable)
			if test.withError != nil {
				if err == nil || !errors.Is(err, test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			_, err = s.Consumer(ctx, test.durable)
			if err == nil || !errors.Is(err, jetstream.ErrConsumerNotFound) {
				t.Fatalf("Expected error: %v; got: %v", jetstream.ErrConsumerNotFound, err)
			}
		})
	}
}

func TestStreamNameBySubject(t *testing.T) {
	tests := []struct {
		name      string
		subject   string
		withError error
		timeout   time.Duration
		expected  string
	}{
		{
			name:     "get stream name by subject explicit",
			subject:  "FOO.123",
			timeout:  10 * time.Second,
			expected: "foo",
		},
		{
			name:     "with empty context",
			subject:  "FOO.123",
			expected: "foo",
		},
		{
			name:     "get stream name by subject with wildcard",
			subject:  "BAR.*",
			expected: "bar",
		},
		{
			name:     "match more than one stream, return the first one",
			subject:  ">",
			expected: "",
		},
		{
			name:      "stream not found",
			subject:   "BAR.XYZ",
			withError: jetstream.ErrStreamNotFound,
		},
		{
			name:      "invalid subject",
			subject:   "FOO.>.123",
			withError: jetstream.ErrInvalidSubject,
		},
		{
			name:      "context timeout",
			subject:   "FOO.123",
			timeout:   1 * time.Microsecond,
			withError: context.DeadlineExceeded,
		},
	}

	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()

	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "bar", Subjects: []string{"BAR.ABC"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), test.timeout)
				defer cancel()
			}
			name, err := js.StreamNameBySubject(ctx, test.subject)
			if test.withError != nil {
				if err == nil || !errors.Is(err, test.withError) {
					t.Fatalf("Expected error: %v; got: %v", test.withError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if test.expected != "" && name != test.expected {
				t.Fatalf("Unexpected stream name; want: %s; got: %s", test.expected, name)
			}
		})
	}
}
