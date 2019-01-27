s3uploader
==========

Simple tool to encrypt and upload to S3 with badnwidth throttling.

I needed an easy way to upload data to S3 on various ARM-powered nodes (NAS,
Raspberry etc). However, data should be encrypted and bandwidth usage should be
limited, and I didn't want to install tons of packages on these nodes.


Building
--------
    $ go get github.com/cschwede/s3uploader
    $ go build github.com/cschwede/s3uploader

If you want to cross-compile this to make it work on other architectures:

    $ env GOOS=linux GOARCH=arm GOARM=5 go build github.com/cschwede/s3uploader

See [list of supported architectures](https://github.com/golang/go/wiki/GoArm#supported-architectures).

Usage
-----

    ./s3uploader -b <bucket name> -p <GPG public key file> -f <file to upload> -kbps <max bandwidth in kbps> -k <S3 key name to use>
