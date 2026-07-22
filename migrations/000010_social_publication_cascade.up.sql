ALTER TABLE social_publications
    DROP CONSTRAINT social_publications_social_connection_id_fkey;

ALTER TABLE social_publications
    ADD CONSTRAINT social_publications_social_connection_id_fkey
    FOREIGN KEY (social_connection_id)
    REFERENCES social_connections(id)
    ON DELETE CASCADE;
