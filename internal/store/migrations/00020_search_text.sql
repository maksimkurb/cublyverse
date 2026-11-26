-- +goose Up
-- The form search compares: lower case, without Latin diacritics and with ё as
-- е, so "mouline special" finds "Mouliné Spécial". internal/features/craft
-- (searchFold) folds the query the same way.
-- +goose StatementBegin
CREATE FUNCTION craft_search_text(value text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT translate(lower(value),
        'áàâäãåąāčćçďéèêëěęēíìîïīĺľłňńñóòôöõőōřŕšśşťţúùûüůűūýÿžźżё',
        'aaaaaaaacccdeeeeeeeiiiiilllnnnooooooorrsssttuuuuuuuyyzzzе')
$$;
-- +goose StatementEnd
