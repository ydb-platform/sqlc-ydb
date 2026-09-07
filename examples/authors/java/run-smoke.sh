#!/bin/sh
set -eu

if [ -z "${SQLC_YDB_TEST_DSN:-}" ]; then
    echo "SQLC_YDB_TEST_DSN is required" >&2
    exit 2
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
AUTHORS_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
cd "$AUTHORS_DIR"

mvn -f java/pom.xml -DskipTests test-compile

for module in native jdbc spring hibernate; do
    mvn -q -f "java/$module/pom.xml" dependency:build-classpath \
        -Dmdep.includeScope=test \
        -Dmdep.outputFile="target/smoke-classpath.txt"
    classpath="java/$module/target/test-classes:java/$module/target/classes:$(cat "java/$module/target/smoke-classpath.txt")"
    case "$module" in
        native) class=authors.nativeapi.Smoke ;;
        jdbc) class=authors.jdbc.Smoke ;;
        spring) class=authors.spring.Smoke ;;
        hibernate) class=authors.hibernate.Smoke ;;
    esac
    echo "Running $class"
    java -cp "$classpath" "$class"
done
