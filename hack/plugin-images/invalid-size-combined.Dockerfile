FROM busybox:1.31.1 AS builder

RUN mkdir myheader &&\
    dd if=/dev/urandom of=/myheader/handler.lua bs=512k count=1 &&\
    dd if=/dev/urandom of=/myheader/schema.lua bs=512k count=1


FROM scratch

COPY --from=builder /myheader /
