-- name: ColdCities :many
DECLARE $maximum_temperature AS Int32;
SELECT city, COUNT(*) AS reading_count, MAX(temperature) AS hottest_temperature
FROM weather
GROUP BY city
HAVING MAX(temperature) < $maximum_temperature;
