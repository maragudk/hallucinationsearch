create table queries (
  id text primary key default ('q_' || lower(hex(randomblob(16)))),
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  text text unique not null
) strict;

create trigger queries_updated_timestamp after update on queries begin
  update queries set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where id = old.id;
end;

create table results (
  id text primary key default ('r_' || lower(hex(randomblob(16)))),
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  query_id text not null references queries (id) on delete cascade,
  position integer not null check (position between 0 and 9),
  title text not null,
  display_url text not null,
  description text not null,
  unique (query_id, position)
) strict;

create trigger results_updated_timestamp after update on results begin
  update results set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where id = old.id;
end;

create index results_query_id_position_idx on results (query_id, position);

create table websites (
  result_id text primary key references results (id) on delete cascade,
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  html text not null
) strict;

create trigger websites_updated_timestamp after update on websites begin
  update websites set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where result_id = old.result_id;
end;
