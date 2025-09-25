-- persist/sqld/migrate.go (added "if not exists" clauses)
create table if not exists argo_workflows (
    uid varchar(128) not null,
    namespace varchar(256) not null,
    clustername varchar(64) not null,
    version varchar(64),
    nodes json not null,
    updatedat timestamp not null default current_timestamp,

    primary key (clustername, uid, version)
);
create index if not exists argo_workflows_i1 on argo_workflows (clustername, namespace, updatedat);
create table if not exists argo_archived_workflows (
    uid varchar(128) not null,
    name varchar(256) not null,
    phase varchar(25) not null,
    namespace varchar(256) not null,
    workflow jsonb not null,
    startedat timestamp default CURRENT_TIMESTAMP not null,
    finishedat timestamp default CURRENT_TIMESTAMP not null,
    clustername varchar(64) not null,
    instanceid varchar(64) not null,

    primary key (clustername, uid)
);
create index if not exists argo_archived_workflows_i1 on argo_archived_workflows (clustername, instanceid, namespace);
create index if not exists argo_archived_workflows_i2 on argo_archived_workflows (clustername, instanceid, finishedat);
create index if not exists argo_archived_workflows_i3 on argo_archived_workflows (clustername, instanceid, name);
create index if not exists argo_archived_workflows_i4 on argo_archived_workflows (clustername, startedat);
create table if not exists argo_archived_workflows_labels (
    clustername varchar(64) not null,
    uid varchar(128) not null,
    name varchar(317) not null,
    value varchar(63) not null,

    primary key (clustername, uid, name),
    foreign key (clustername, uid) references argo_archived_workflows(clustername, uid) on delete cascade
);
create index if not exists argo_archived_workflows_labels_i1 on argo_archived_workflows_labels (name, value);

--- workflow/sync/migrate.go (added "if not exists" clauses)
create table if not exists sync_limit (
    name varchar(256) not null,
    sizelimit int,
    primary key (name)
);
create unique index if not exists ilimit_name on sync_limit (name);
create table if not exists sync_controller (
    controller varchar(64) not null,
    time timestamp,
    primary key (controller)
);
create unique index if not exists icontroller_name on sync_controller (controller);
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
create table if not exists sync_lock (
    name varchar(256),
    controller varchar(64) not null,
    time timestamp,
    primary key(name)
);
create unique index if not exists ilock_name on sync_lock (name);
