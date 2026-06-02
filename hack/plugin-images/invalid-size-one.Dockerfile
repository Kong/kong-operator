FROM busybox:1.38.0@sha256:011bb4ad411421bd6af53c4af41ecb2c23887229b8df526328d1a8a1fff94dab AS builder

COPY myheader/schema.lua /myheader/
RUN dd if=/dev/urandom of=/myheader/handler.lua bs=1M count=2


FROM scratch

COPY --from=builder /myheader /
