FROM scratch AS builder

# Rename handler.lua to an invalid name.
COPY myheader/handler.lua /myheader/add-header.lua
COPY myheader/schema.lua /myheader/schema.lua

FROM scratch

COPY --from=builder /myheader /
