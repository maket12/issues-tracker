CREATE TABLE Sprints (
  `key`   BIGINT      NOT NULL AUTO_INCREMENT,
  id      VARCHAR(64) NOT NULL,
  `start` DATE        NOT NULL,
  `end`   DATE        NOT NULL,
  closed  BIT(1)      NOT NULL DEFAULT b'0',
  PRIMARY KEY (`key`),
  UNIQUE KEY uq_sprints_id (id),
  CONSTRAINT chk_sprints_dates CHECK (`end` >= `start`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE SprintAssignments (
  Sprintskey BIGINT NOT NULL,
  Issuekey   BIGINT NOT NULL,
  PRIMARY KEY (Sprintskey, Issuekey),
  KEY idx_sprintassignments_issue (Issuekey),
  CONSTRAINT fk_sprintassignments_sprint
    FOREIGN KEY (Sprintskey) REFERENCES Sprints (`key`) ON DELETE CASCADE,
  CONSTRAINT fk_sprintassignments_issue
    FOREIGN KEY (Issuekey) REFERENCES Issue (`key`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
