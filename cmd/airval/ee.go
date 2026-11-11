//go:build ee

package main

// Enterprise features register themselves with internal/ext from init().
import _ "github.com/airval/airval/ee"
