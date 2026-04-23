create table images (
  path_hash text primary key,
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  path text not null,
  mime_type text not null,
  data blob not null
) strict;

create trigger images_updated_timestamp after update on images begin
  update images set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where path_hash = old.path_hash;
end;
