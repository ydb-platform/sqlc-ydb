#!/bin/sh
set -eu

: "${SQLC_YDB_TEST_DSN:?SQLC_YDB_TEST_DSN is required}"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$SCRIPT_DIR/.."

mvn -f kotlin/pom.xml test-compile
mvn -q -f kotlin/pom.xml dependency:build-classpath \
    -Dmdep.includeScope=test -Dmdep.outputFile=target/smoke-classpath.txt
classpath="kotlin/target/test-classes:kotlin/target/classes:$(cat kotlin/target/smoke-classpath.txt)"
java -cp "$classpath" authors.smoke.SmokeKt
