// This is a new package
package main

import (
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	_ "crypto/sha256"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"
	_ "golang.org/x/crypto/ripemd160"
)

func main() {
	var bucket, key, filename, pubkey string
	var kbps int

	flag.StringVar(&bucket, "b", "", "Bucket name.")
	flag.StringVar(&key, "k", "", "Object key name.")
	flag.StringVar(&filename, "f", "", "Filename.")
	flag.StringVar(&pubkey, "p", "", "Public key file")
	flag.IntVar(&kbps, "kbps", 1<<31-1, "Kilobytes per second")
	flag.Parse()

	if filename == "" || pubkey == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	svc := getService(bucket)
	if !objectExists(svc, bucket, key) {
		tmpfile := encryptFile(filename, pubkey)
		defer os.Remove(tmpfile)

		upload(tmpfile, svc, bucket, key, kbps)
	} else {
		log.Printf("skipped uploading file %s to %s/%s\n", filename, bucket, key)
	}
}

func getService(bucket string) *s3.S3 {
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String("us-east-1"),
	}))
	svc := s3.New(sess)

	resp, err := svc.GetBucketLocation(&s3.GetBucketLocationInput{
		Bucket: &bucket,
	})
	checkErr(err)

	sess = session.Must(session.NewSession(&aws.Config{
		Region: resp.LocationConstraint,
	}))
	svc = s3.New(sess)

	return svc
}

func upload(filename string, svc *s3.S3, bucket string, key string, kbps int) {
	file, ferr := os.Open(filename)
	checkErr(ferr)
	defer file.Close()

	req, _ := svc.PutObjectRequest(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	url, _ := req.Presign(5 * time.Minute)

	request, _ := http.NewRequest("PUT", url, SlowReader(file, kbps*1024))

	// Need to properly set the content length, otherwise S3 fails
	fi, err := file.Stat()
	checkErr(err)
	request.ContentLength = fi.Size()

	begin := time.Now()
	response, err := http.DefaultClient.Do(request)
	checkErr(err)
	defer response.Body.Close()
	realKbps := float32(fi.Size()/1024) / float32(time.Since(begin)/time.Second)

	if response.StatusCode == 200 {
		log.Printf("successfully uploaded file %s to %s/%s (%f kbps)\n", filename, bucket, key, realKbps)
	}
}

// encryptFile takes a filename and path to a public key, creates a temporary
// file and encrypts it using the public key It returns the path to the
// encrypted file
func encryptFile(filename string, pubkey string) string {
	file, ferr := os.Open(filename)
	checkErr(ferr)
	defer file.Close()

	entity := entityFromFile(pubkey)

	cipherfile, err := ioutil.TempFile("", "uploader")
	checkErr(err)
	defer cipherfile.Close()

	filehint := &openpgp.FileHints{
		IsBinary: true,
		FileName: filepath.Base(filename),
	}

	plaintext, err := openpgp.Encrypt(cipherfile, []*openpgp.Entity{entity}, nil, filehint, nil)
	checkErr(err)
	defer plaintext.Close()

	buf := make([]byte, 1024)
	for {
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			checkErr(err)
		}
		if n == 0 {
			break
		}
		plaintext.Write(buf[:n])
	}

	return cipherfile.Name()
}

func entityFromFile(filename string) *openpgp.Entity {
	in, err := os.Open(filename)
	checkErr(err)
	defer in.Close()

	block, err := armor.Decode(in)
	checkErr(err)

	reader := packet.NewReader(block.Body)
	pkt, err := reader.Next()
	checkErr(err)

	pubKey, _ := pkt.(*packet.PublicKey)

	entity := openpgp.Entity{
		PrimaryKey: pubKey,
		Identities: make(map[string]*openpgp.Identity),
	}

	entity.Identities["default"] = &openpgp.Identity{
		SelfSignature: &packet.Signature{},
	}

	entity.Subkeys = make([]openpgp.Subkey, 1)
	entity.Subkeys[0] = openpgp.Subkey{
		PublicKey: pubKey,
		Sig:       &packet.Signature{},
	}
	return &entity
}

type SlowReaderType struct {
	file           *os.File
	bytesPerSecond int
	bytesRead      int
	localBuffer    []byte
}

func SlowReader(file *os.File, bytesPerSecond int) *SlowReaderType {
	buf := make([]byte, bytesPerSecond)
	return &SlowReaderType{file, bytesPerSecond, 0, buf}
}

func (rdr *SlowReaderType) Read(buffer []byte) (bytesRead int, err error) {
	// read max bytesPerSecond at once
	if len(buffer) > len(rdr.localBuffer) {
		bytesRead, err = rdr.file.Read(rdr.localBuffer)
		copy(buffer, rdr.localBuffer)
	} else {
		bytesRead, err = rdr.file.Read(buffer)
	}

	// Sleep up to a second, depending on how much data has been read
	fraction := float32(bytesRead) / float32(rdr.bytesPerSecond)
	sleep := time.Duration(fraction*1000) * time.Millisecond
	time.Sleep(sleep)

	return bytesRead, err
}

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func objectExists(svc *s3.S3, bucket string, key string) bool {
	resp, _ := svc.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if resp.LastModified != nil {
		return true
	}
	return false
}

func getExistingObjects(bucket string) map[string]bool {
	svc := getService(bucket)

	objs := make(map[string]bool)

	err := svc.ListObjectsPages(&s3.ListObjectsInput{
		Bucket: aws.String(bucket),
	}, func(p *s3.ListObjectsOutput, last bool) (shouldContinue bool) {
		for _, obj := range p.Contents {
			objs[*obj.Key] = true
		}
		return true
	})
	checkErr(err)
	return objs
}
