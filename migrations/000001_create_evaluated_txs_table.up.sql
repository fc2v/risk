create table if not exists evaluated_txs (
    id text primary key,
    account_id text not null,
    transaction_type text not null check (transaction_type in ('card', 'debitOrder', 'transfer')),
    kind text not null check (kind in ('card', 'cardRefund', 'debitOrder', 'transferOut', 'transferIn', 'ownAccountTransfer')),
    amount bigint not null,
    created_at timestamp(0) with time zone not null default now(),
    posted_at timestamp with time zone not null,
    category text not null default '',
    details jsonb not null,
    score integer not null check (score between 0 and 100),
    risk text not null check (risk in ('UNKNOWN', 'LOW', 'MEDIUM', 'HIGH')),
    rule_outcomes jsonb not null default '[]'
);

create index if not exists evaluated_txs_account_posted_idx
    on evaluated_txs (account_id, posted_at desc);

create index if not exists evaluated_txs_account_kind_posted_idx
    on evaluated_txs (account_id, kind, posted_at desc);

create table if not exists account_hours (
    account_id text not null,
    kind text not null,
    hour timestamp with time zone not null,
    tx_count integer not null,
    inflow bigint not null,
    outflow bigint not null,
    with_device integer not null,
    primary key (account_id, kind, hour)
);

create table if not exists account_seen (
    account_id text not null,
    field text not null check (field in ('counterparty', 'device', 'merchantCountry', 'mcc', 'creditor')),
    value text not null,
    kind text not null,
    first_seen timestamp with time zone not null,
    last_seen timestamp with time zone not null,
    primary key (account_id, field, value, kind)
);
