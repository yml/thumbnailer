# Thumbnailer

This package is still work in progress. The intend is to provides tools to create thumbnails :

* Using an http server
* Using a real time distributed messaging platform (nsq)

## TODO

* [x] make libjpeg-turbo optional
* [x] add tests
* [ ] make aws s3 optionnal
* [ ] add documentation
* [ ] memory profiling
* [ ] Add an http endpoint that redirect to the image

## Dependencies

Thumbnailer has an optional dependency on libjpeg-
* On Ubuntu: sudo apt-get install libjpeg-turbo8-dev.
* On Mac OS X: brew install libjpeg-turbo

This feature is guarded by a build tag called `libjpegturbo`

```
go install -tags libjpegturbo github.com/yml/thumbnailer/...
```

**Note:** Don't forget to clean up the pkg dir between build

## How to use it


## nsq_thumbnailer

nsq based consumer that  generates thumbnails.

### Start nsq machinery in 3 terminals

```
nsqlookupd
nsqd --lookupd-tcp-address=127.0.0.1:4160
nsqadmin --lookupd-http-address=127.0.0.1:4161
```

### How to debug nsq messages

You can tail the messages flowing in the test topic

```
nsq_tail -lookupd-http-address=127.0.0.1:4161 -topic=test
```


### Start nsq thumbnailing service

```
export AWS_SECRET_ACCESS_KEY=<secret>
export AWS_ACCESS_KEY_ID=<secret>

go install github.com/yml/thumbnailer/... && nsq_thumbnailer --topic=test --lookupd-http-address=127.0.0.1:4161 --concurrency=10
```


### send nsq message

#### local file system

```
curl -d '{"srcImage": "file://tmp/nsq-thumb-src-test/image1.jpg, "opts": [{"rect":{"min":[200, 200], "max":[600,600]},"width":150, "height":0}, {"width":250, "height":0}, {"width":0, "height":350}], "dstFolder":""file://tmp/nsq-thumb-dst-test/}' 'http://127.0.0.1:4151/put?topic=test'
```

#### S3

```
curl -d '{"srcImage": "s3://nsq-thumb-src-test/baignade.jpg", "opts": [{"rect":{"min":[200, 200], "max":[600,600]},"width":150, "height":0}, {"width":250, "height":0}, {"width":0, "height":350}], "dstFolder":"s3://nsq-thumb-dst-test/"}' 'http://127.0.0.1:4151/put?topic=test'
```

## http_thumbnailer

http thumbnailer

### Start the service

```
export AWS_SECRET_ACCESS_KEY=<secret>
export AWS_ACCESS_KEY_ID=<secret>

go install github.com/yml/thumbnailer/... && http_thumbnailer -dstFolder=file:///tmp/nsq-thumb-dst-test -srcFolder=file:///tmp/nsq-thumb-src-test 
```

### send http request to generate thumbnails


```
yml@garfield$ (git: http_thumbnailer) curl 127.0.0.1:9900/thumb/50x50/baignade.jpg

[{"Thumbnail":{"Scheme":"file","Opaque":"","User":null,"Host":"","Path":"/home/yml/Dropbox/Devs/golang/nsq_sandbox/nsq-thumb-dst-test/baignade_s50x50.jpg","RawQuery":"","Fragment":""},"Err":null}]
```

```
curl 127.0.0.1:9900/thumbs/ -d '{"srcImage": "s3://nsq-thumb-src-test/baignade.jpg", "opts": [{"width":0, "height":350}], "dstFolder":"s3://nsq-thumb-dst-test/"}'

[{"Thumbnail":{"Scheme":"s3","Opaque":"","User":null,"Host":"nsq-thumb-dst-test","Path":"/baignade_s467x350.jpg","RawQuery":"","Fragment":""},"Err":null}]
```

