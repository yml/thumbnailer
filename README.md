#nsqthumbnailer

nsq based consumer that  generates thumbnails.

## Nothing to see in there yet.

## How to use it

### Start nsq machinery in 3 terminals

```
nsqlookupd 
nsqd --lookupd-tcp-address=127.0.0.1:4160
nsqadmin --lookupd-http-address=127.0.0.1:4161
```

## How to debug

You can tail the messages flowing in the test topic

```
nsq_tail -lookupd-http-address=127.0.0.1:4161 -topic=test
```

## Start nsq_thumbnailer

```
go run main.go --topic=test --lookupd-http-address=127.0.0.1:4161
```

## send messages

local file system

```
curl -d '{"SrcImage": "file://home/yml/Desktop/a_importer/image1.jpg", "Opts": [{"Width":100, "Height":0}], "DstFolder":"file://home/yml/Dropbox/Devs/golang/nsq_sandbox/src/github.com/yml/nsqthumbnailer"}' 'http://127.0.0.1:4151/put?topic=test'
```

S3

```
export AWS_SECRET_ACCESS_KEY=<secret>
export AWS_ACCESS_KEY_ID=<secret>

curl -d '{"SrcImage": "s3://nsq-thumb-src-test/baignade.jpg", "Opts": [{"Width":150, "Height":0}, {"Width":250, "Height":0}, {"Width":0, "Height":350}], "DstFolder":"s3://nsq-thumb-dst-test/"}' 'http://127.0.0.1:4151/put?topic=test'
```

