# Supply an already-installed Alpine-compatible tag/ID. Build with --network=none.
ARG BASE
FROM ${BASE}
COPY multica /usr/local/bin/multica
COPY fake-agent.sh /usr/local/bin/fixture-agent
RUN chmod 755 /usr/local/bin/multica /usr/local/bin/fixture-agent
