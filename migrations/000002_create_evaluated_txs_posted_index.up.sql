create index concurrently if not exists evaluated_txs_posted_idx
    on evaluated_txs (posted_at desc, id desc);
