# Go type overrides

This example gives physical `customers.id` the domain type `CustomerID`, all required `Utf8` values the domain type `CustomerName`, and optional `Utf8` values `*Note`. It generates both native YDB and `database/sql` clients from the same schema and queries. The types in `domain` have the same underlying Go types as their YQL values, so generated methods convert inputs to the SDK types and decode results into the domain types.
