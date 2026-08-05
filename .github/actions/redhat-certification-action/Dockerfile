FROM docker:29.7@sha256:e8faad5a8dc5279dff929afc5449f2791736912fff9f99351d742db2fad01b4c

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
