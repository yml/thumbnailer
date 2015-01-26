# bulk-loader

`bulk-loader` walk a directory and emit a request to `nsq_thumbnailer` to
generate the specified thumbs.

```
go run main.go -post-url="http://127.0.0.1:4151/put?topic=test" -src-directory=file:///tmp/nsq-thumb-src-test/ -dst-directory=s3://nsq-thumb-dst-test/ -thumbnail-options='[{"rect":{"min":[200, 200], "max":[600,600]},"width":150, "height":0}, {"width":250, "height":0}]'
```

## TODO

* [ ] Add support to walk an S3 container
