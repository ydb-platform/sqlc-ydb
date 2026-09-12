# Shared PHP example harness

This Composer project checks and runs the generated PHP code for authors, batch, booktest, jets and ondeck. Its classmap covers each `php/native` output directory because every generated `YdbRuntime.php` intentionally contains the internal `YdbRawExecutor` and `YdbValueCodec` classes. A generated `Queries.php` also loads that runtime file directly, so consuming applications that use ordinary PSR-4 loading can construct `Queries` without adding a special classmap rule for the internal classes.
