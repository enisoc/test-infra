/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mungers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	github_util "k8s.io/test-infra/mungegithub/github"
	github_test "k8s.io/test-infra/mungegithub/github/testing"
	"k8s.io/test-infra/mungegithub/mungeopts"
	"k8s.io/test-infra/mungegithub/options"

	"github.com/golang/glog"
	"github.com/google/go-github/github"
)

const (
	jenkinsE2EContext    = "Jenkins GCE e2e"
	jenkinsUnitContext   = "Jenkins unit/integration"
	jenkinsVerifyContext = "Jenkins verification"
	jenkinsNodeContext   = "Jenkins GCE Node e2e"
)

var (
	_ = fmt.Printf
	_ = glog.Errorf

	requiredContexts = []string{
		jenkinsUnitContext,
		jenkinsE2EContext,
		jenkinsNodeContext,
		jenkinsVerifyContext,
	}
)

func timePtr(t time.Time) *time.Time { return &t }

func NowStatus() *github.CombinedStatus {
	status := github_test.Status("mysha", requiredContexts, nil, nil, nil)
	for i := range status.Statuses {
		s := &status.Statuses[i]
		s.CreatedAt = timePtr(time.Now())
		s.UpdatedAt = timePtr(time.Now())
	}
	return status
}

func OldStatus() *github.CombinedStatus {
	return github_test.Status("mysha", requiredContexts, nil, nil, nil)
}

func TestOldUnitTestMunge(t *testing.T) {
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Avoid a 5 second delay
	github_util.SetCombinedStatusLifetime(time.Millisecond)

	tests := []struct {
		name     string
		tested   bool
		ciStatus *github.CombinedStatus
	}{
		{
			name:     "Test0",
			tested:   true,
			ciStatus: OldStatus(), // Ran at time.Unix(0,0)
		},
		{
			name:     "Test1",
			tested:   false,
			ciStatus: NowStatus(), // Ran at time.Unix(0,0)
		},
	}
	for testNum, test := range tests {
		issueNum := testNum + 1
		tested := false

		issue := LGTMIssue()
		issue.Number = intPtr(issueNum)
		pr := ValidPR()
		pr.Number = intPtr(issueNum)
		client, server, mux := github_test.InitServer(t, issue, pr, nil, nil, test.ciStatus, nil, nil)

		path := fmt.Sprintf("/repos/o/r/issues/%d/comments", issueNum)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("Unexpected method: %s", r.Method)
			}

			type comment struct {
				Body string `json:"body"`
			}
			c := new(comment)
			json.NewDecoder(r.Body).Decode(c)
			msg := c.Body
			if strings.HasPrefix(msg, "/test all") {
				tested = true
				test.ciStatus.State = stringPtr("pending")
				for id := range test.ciStatus.Statuses {
					status := &test.ciStatus.Statuses[id]
					if *status.Context == jenkinsE2EContext || *status.Context == jenkinsUnitContext {
						status.State = stringPtr("pending")
						break
					}
				}

			}
			w.WriteHeader(http.StatusOK)
			data, err := json.Marshal(github.IssueComment{})
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			w.Write(data)
		})

		config := &github_util.Config{
			Org:          "o",
			Project:      "r",
			BaseWaitTime: time.Nanosecond, // Avoid a 30 second delay
		}
		config.SetClient(client)

		s := StaleGreenCI{}
		err := s.Initialize(config, nil)
		if err != nil {
			t.Fatalf("%v", err)
		}

		obj, err := config.GetObject(issueNum)
		if err != nil {
			t.Fatalf("%v", err)
		}

		s.opts = options.New()
		mungeopts.RequiredContexts.Retest = requiredContexts
		s.Munge(obj)

		if tested != test.tested {
			t.Errorf("%d:%s tested=%t but should be %t", testNum, test.name, tested, test.tested)
		}
		server.Close()
	}
}
