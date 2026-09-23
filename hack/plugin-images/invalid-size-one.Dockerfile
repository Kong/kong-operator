FROM busybox:1.38.0@sha256:79b6957454b0506796c042e9e2dfedd996c7efc93e701166d2dc9569bc9cc096 AS builder

COPY myheader/schema.lua /myheader/
RUN dd if=/dev/urandom of=/myheader/handler.lua bs=1M count=2


FROM scratch

COPY --from=builder /myheader /
