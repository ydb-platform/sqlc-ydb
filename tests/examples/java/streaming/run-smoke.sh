#!/bin/sh
set -eu
: "${YDB_CONNECTION_STRING:?Set YDB_CONNECTION_STRING to a disposable database without a streaming_devices table}"
cd "$(dirname "$0")"
mvn -B test-compile dependency:build-classpath -Dmdep.outputFile=target/classpath
java -cp "target/test-classes:target/classes:$(cat target/classpath)" StreamingSmoke
