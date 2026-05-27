FROM busybox:1.38.0@sha256:011bb4ad411421bd6af53c4af41ecb2c23887229b8df526328d1a8a1fff94dab AS builder

COPY myheader /myheader/
RUN sed -i 's/"myheader"/"newheader"/g' /myheader/**
RUN sed -i 's/"roar"/"amazing"/g' /myheader/**


FROM scratch

COPY --from=builder /myheader /
