FROM busybox:1.38.0@sha256:011bb4ad411421bd6af53c4af41ecb2c23887229b8df526328d1a8a1fff94dab AS builder

RUN mkdir myheader &&\
    dd if=/dev/urandom of=/myheader/handler.lua bs=512k count=1 &&\
    dd if=/dev/urandom of=/myheader/schema.lua bs=512k count=1


FROM scratch

COPY --from=builder /myheader /
