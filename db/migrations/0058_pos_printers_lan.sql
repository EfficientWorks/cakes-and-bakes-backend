-- +goose Up

-- LAN (wired Ethernet) printers.
--
-- Network-addressed like Wi-Fi: needs an IP, one printer per IP per outlet,
-- and several may be connected at once. Only Bluetooth keeps the
-- one-connected-at-a-time rule.

ALTER TABLE pos_printers DROP CONSTRAINT IF EXISTS pos_printers_kind_check;
ALTER TABLE pos_printers
  ADD CONSTRAINT pos_printers_kind_check CHECK (kind IN ('wifi', 'lan', 'bluetooth'));

ALTER TABLE pos_printers DROP CONSTRAINT IF EXISTS pos_printers_wifi_needs_ip;
ALTER TABLE pos_printers
  ADD CONSTRAINT pos_printers_wifi_needs_ip
  CHECK (kind = 'bluetooth' OR (ip_address IS NOT NULL AND length(btrim(ip_address)) > 0));

DROP INDEX IF EXISTS pos_printers_branch_ip_idx;
CREATE UNIQUE INDEX pos_printers_branch_ip_idx
  ON pos_printers (branch_id, lower(btrim(ip_address)))
  WHERE kind IN ('wifi', 'lan') AND ip_address IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS pos_printers_branch_ip_idx;
CREATE UNIQUE INDEX pos_printers_branch_ip_idx
  ON pos_printers (branch_id, lower(btrim(ip_address)))
  WHERE kind = 'wifi' AND ip_address IS NOT NULL;

ALTER TABLE pos_printers DROP CONSTRAINT IF EXISTS pos_printers_wifi_needs_ip;
ALTER TABLE pos_printers
  ADD CONSTRAINT pos_printers_wifi_needs_ip
  CHECK (kind <> 'wifi' OR (ip_address IS NOT NULL AND length(btrim(ip_address)) > 0));

ALTER TABLE pos_printers DROP CONSTRAINT IF EXISTS pos_printers_kind_check;
ALTER TABLE pos_printers
  ADD CONSTRAINT pos_printers_kind_check CHECK (kind IN ('wifi', 'bluetooth'));