-- Create keyspace (RF=1 for dev, RF=3 for production)
CREATE KEYSPACE IF NOT EXISTS orchestration
  WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

-- Primary event table
CREATE TABLE IF NOT EXISTS orchestration.events (
    subject         TEXT,
    time            TIMESTAMP,
    id              TEXT,
    type            TEXT,
    source          TEXT,
    datacontenttype TEXT,
    data            TEXT,
    extensions      MAP<TEXT, TEXT>,
    PRIMARY KEY (subject, time)
) WITH CLUSTERING ORDER BY (time ASC)
  AND compaction = {'class': 'TimeWindowCompactionStrategy',
                    'compaction_window_size': 1,
                    'compaction_window_unit': 'DAYS'};
