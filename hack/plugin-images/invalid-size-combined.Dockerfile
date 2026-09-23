FROM busybox:1.38.0@sha256:79b6957454b0506796c042e9e2dfedd996c7efc93e701166d2dc9569bc9cc096 AS builder

RUN mkdir myheader &&\
    dd if=/dev/urandom of=/myheader/handler.lua bs=512k count=1 &&\
    dd if=/dev/urandom of=/myheader/schema.lua bs=512k count=1


FROM scratch

COPY --from=builder /myheader /
