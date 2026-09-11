#!/bin/sh
set -eu

if [ -z "${YDB_CONNECTION_STRING:-}" ]; then
    echo "YDB_CONNECTION_STRING is required" >&2
    exit 2
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
AUTHORS_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
cd "$AUTHORS_DIR"

mvn -f java/pom.xml -DskipTests test-compile

for module in native jdbc; do
    mvn -q -f "java/$module/pom.xml" dependency:build-classpath \
        -Dmdep.includeScope=test \
        -Dmdep.outputFile="target/smoke-classpath.txt"
    classpath="java/$module/target/test-classes:java/$module/target/classes:$(cat "java/$module/target/smoke-classpath.txt")"
    case "$module" in
        native) class=authors.nativeapi.Smoke ;;
        jdbc) class=authors.jdbc.Smoke ;;
    esac
    echo "Running $class"
    java -cp "$classpath" "$class"
done
