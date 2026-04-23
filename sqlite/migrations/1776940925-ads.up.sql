create table ads (
  id text primary key default ('a_' || lower(hex(randomblob(16)))),
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  query_id text not null references queries (id) on delete cascade,
  position integer not null check (position between 0 and 2),
  title text not null,
  display_url text not null,
  description text not null,
  sponsor text not null,
  cta text not null,
  unique (query_id, position)
) strict;

create trigger ads_updated_timestamp after update on ads begin
  update ads set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where id = old.id;
end;

create index ads_query_id_position_idx on ads (query_id, position);

create table ad_websites (
  ad_id text primary key references ads (id) on delete cascade,
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  html text not null
) strict;

create trigger ad_websites_updated_timestamp after update on ad_websites begin
  update ad_websites set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where ad_id = old.ad_id;
end;
