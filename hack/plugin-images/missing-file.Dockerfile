FROM scratch

# File schema.lua will be missing in the final image.
COPY myheader/handler.lua /
