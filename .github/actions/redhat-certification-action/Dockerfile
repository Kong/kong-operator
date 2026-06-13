FROM docker:29.5@sha256:ad68e89b675740867a3bb96488a93fea9209ad36c6305bfba2664912d6dcf11a

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
