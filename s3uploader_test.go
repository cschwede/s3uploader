package main

import (
	"io/ioutil"
	"math/rand"
    "os"
	"testing"
	"time"
)

func TestSlowReader(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	tmpfile, err := ioutil.TempFile("", "s3uploader_test")
	checkErr(err)
	defer tmpfile.Close()
    defer os.Remove(tmpfile.Name())

	data := make([]byte, 1000)
	rand.Read(data)
	tmpfile.Write(data)

	tmpfile.Seek(0, 0)

	buf := make([]byte, 100)
	rdr := SlowReader(tmpfile, 20000)

	start := time.Now()
	for {
		n, err := rdr.Read(buf)
		_ = err
		if n == 0 {
			break
		}
	}
	elapsed := time.Since(start)
	if time.Duration.Seconds(elapsed) < 0.05 {
		t.Error("Data read too fast")
	}
}
