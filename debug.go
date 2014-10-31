// +build !release

package main

import "github.com/bugsnag/bugsnag-go"

func init() {

	bugsnag.Configure(bugsnag.Configuration{
		APIKey:       "205838b03710e9d7bf45b3722d7b9ac6",
		ReleaseStage: "development",
	})
}
