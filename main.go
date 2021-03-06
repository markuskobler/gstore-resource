package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

var cmd string

func init() {
	flag.StringVar(&cmd, "cmd", filepath.Base(os.Args[0]), "check, in, out")
}

func main() {
	flag.Parse()
	Runner{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}.Exec(cmd, flag.Args()...)
}

type Runner struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (r Runner) Exec(cmd string, args ...string) {
	switch cmd {
	case "check":
		fmt.Fprintf(r.Stdout, `{"version":[]}`)

	case "in":
		if len(args) != 1 {
			r.Fail("usage: in <destination>")
		}
		destination := args[0]

		var req InRequest
		err := json.NewDecoder(os.Stdin).Decode(&req)
		if err != nil {
			r.Fail("invalid JSON request: %s", err)
		}

		resp := execIn(&r, destination, req)

		err = json.NewEncoder(os.Stdout).Encode(&resp)
		if err != nil {
			r.Fail("invalid JSON response: %s", err)
		}

	case "out":
		if len(args) != 1 {
			r.Fail("usage: out <source>")
		}
		source := args[0]

		var req OutRequest
		err := json.NewDecoder(r.Stdin).Decode(&req)
		if err != nil {
			r.Fail("invalid JSON request: %s", err)
		}

		resp := execOut(&r, source, req)

		err = json.NewEncoder(r.Stdout).Encode(&resp)
		if err != nil {
			r.Fail("invalid JSON response: %s", err)
		}

	default:
		r.Fail("unexpected command %s; must be check, in, out", cmd)
	}
}

func (r *Runner) Log(msg string, args ...interface{}) {
	fmt.Fprintf(r.Stderr, msg, args...)
	fmt.Fprintln(r.Stderr)
}

func (r *Runner) Fail(msg string, args ...interface{}) {
	r.Log(msg, args...)
	os.Exit(1)
}

type InRequest struct {
	Source  Source           `json:"source"`
	Version TimestampVersion `json:"version"`
	Params  OutParams        `json:"params"`
}

type InResponse struct {
	Version TimestampVersion `json:"version"`
}

type OutRequest struct {
	Source  Source           `json:"source"`
	Version TimestampVersion `json:"version"`
	Params  OutParams        `json:"params"`
}

type Source struct {
	URL         string `json:"url"`
	Credentials string `json:"credentials"`
}

type Version struct {
	Ref string `json:"ref"`
}

type OutParams struct {
	Source string `json:"source"`
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
}

type OutResponse struct {
	Version  TimestampVersion `json:"version"`
	Metadata Metadata         `json:"metadata,omitempty"`
}

type TimestampVersion struct {
	Timestamp string `json:"timestamp"`
}

type Metadata []MetadataField

type MetadataField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

var now = func() int {
	return int(time.Now().Unix())
}

func execIn(r *Runner, destination string, req InRequest) (resp InResponse) {
	if req.Version.Timestamp != "" {
		resp.Version = req.Version
	} else {
		resp.Version.Timestamp = "none"
	}
	return
}

func execOut(r *Runner, source string, req OutRequest) (resp OutResponse) {
	resp.Version.Timestamp = strconv.Itoa(now())
	root := filepath.Join(source, req.Params.Source)

	var files []string

	r.Log("Scan directory %s", root)
	filepath.Walk(root, func(f string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, f)
		}
		return nil
	})

	if len(files) == 0 {
		// TODO: return empty?
		return
	}

	ctx := context.Background()

	var client *storage.Client
	var err error

	if req.Source.Credentials != "" {
		f := filepath.Join("/tmp", "creds")
		e := ioutil.WriteFile(f, []byte(req.Source.Credentials), 0600)
		if e != nil {
			r.Fail("Failed to write creds file %s - %s", f, e)
		}
		client, err = storage.NewClient(ctx, option.WithCredentialsFile(f))
	}

	if client == nil {
		client, err = storage.NewClient(ctx)
	}

	if err != nil {
		r.Fail("google storage failed: %s", err)
		return
	}
	// TODO check the existing objects

	for _, path := range files {
		err = writeFile(r, client, req.Params.Bucket, req.Params.Prefix, root, path)
		if err != nil {
			r.Fail("Failed to write %s: %s", path, err)
		}
	}

	return
}

func writeFile(r *Runner, client *storage.Client, bucket, prefix, root, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}

	name, _ := filepath.Rel(root, path)

	r.Log(">> %s", name)

	obj := client.Bucket(bucket).Object(filepath.Join(prefix, name))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	writer := obj.NewWriter(ctx)
	writer.ContentType = "binary/octet-stream"
	writer.CacheControl = "private, max-age=0"

	_, err = io.Copy(writer, file)
	if err != nil {
		writer.Close()
		return err
	}
	err = writer.Close()
	if err != nil {
		return err
	}

	attr := writer.Attrs()

	r.Log(">> %s (generation: %x, crc: %x, md5: %s)", name, attr.Generation, attr.CRC32C, hex.EncodeToString(attr.MD5))

	return nil
}
