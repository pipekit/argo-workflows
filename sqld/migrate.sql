-- workflow/sync/migrate.go:17
create table if not exists sync_limits (
    name varchar(256) not null,
    sizelimit int,
    primary key (name)
);
create unique index if not exists ilimit_name on sync_limits (name);

-- workflow/sync/migrate.go:23
create table if not exists sync_controller (
    controller varchar(64) not null,
    time timestamp,
    primary key (controller)
);
create unique index if not exists icontroller_name on sync_controller (controller);

-- workflow/sync/migrate.go:29
create table if not exists sync_state (
    name varchar(256),
    workflowkey varchar(256),
    controller varchar(64) not null,
    held boolean,
    priority int,
    time timestamp,
    primary key(name, workflowkey, controller)
);

create index if not exists istate_name on sync_state (name);
create index if not exists istate_workflowkey on sync_state (workflowkey);
create index if not exists istate_controller on sync_state (controller);
create index if not exists istate_held on sync_state (held);

-- workflow/sync/migrate.go:42
create table if not exists sync_lock (
    name varchar(256),
    controller varchar(64) not null,
    time timestamp,
    primary key(name)
);
create unique index if not exists ilock_name on sync_lock (name);
