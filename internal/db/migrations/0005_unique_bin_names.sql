-- CSV import matches bins by exact name (find-or-create). Without a
-- uniqueness guarantee, two bins sharing a name make that match ambiguous:
-- import would silently attach recovered items to an arbitrary one of them.
-- Enforcing uniqueness up front removes that ambiguity, in addition to being
-- useful in its own right.
CREATE UNIQUE INDEX idx_bins_name ON bins(name);
