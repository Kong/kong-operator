FROM busybox:1.31.1 AS builder

COPY myheader/schema.lua /myheader/
RUN dd if=/dev/urandom of=/myheader/handler.lua bs=1M count=2


FROM scratch

COPY --from=builder /myheader /
