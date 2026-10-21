-- name: GetVehicleByUserID :one
SELECT * FROM vehicles
WHERE user_id = $1;

-- name: PutVehicle :one
-- ON CONFLICT covers user_id only, so a plate another driver has still fails, on the
-- vehicles_plate_key constraint with code 23505. old is NULL for an inserted row.
INSERT INTO vehicles (
  user_id, make, model, colour, plate
) VALUES (
  $1, $2, $3, $4, $5
)
ON CONFLICT (user_id) DO UPDATE
SET make = excluded.make, model = excluded.model, colour = excluded.colour, plate = excluded.plate
RETURNING (old.user_id IS NULL)::boolean AS created;
