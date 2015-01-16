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
go run main.go --topic=test --lookupd-http-address=127.0.0.1:4161 --concurrency=10
```

## send messages

local file system

```
curl -d '{"srcImage": "file://tmp/nsqthumbnailer/src/image1.jpg, "opts": [{"rect":{"min":[200, 200], "max":[600,600]},"width":150, "height":0}, {"width":250, "height":0}, {"width":0, "height":350}], "dstFolder":""file://tmp/nsqthumbnailer/tumbs}' 'http://127.0.0.1:4151/put?topic=test'
```

S3

```
export AWS_SECRET_ACCESS_KEY=<secret>
export AWS_ACCESS_KEY_ID=<secret>

curl -d '{"srcImage": "s3://nsq-thumb-src-test/baignade.jpg", "opts": [{"rect":{"min":[200, 200], "max":[600,600]},"width":150, "height":0}, {"width":250, "height":0}, {"width":0, "height":350}], "dstFolder":"s3://nsq-thumb-dst-test/"}' 'http://127.0.0.1:4151/put?topic=test'
```
