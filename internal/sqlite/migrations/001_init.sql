CREATE TABLE students (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    full_name TEXT NOT NULL,
    program TEXT NOT NULL,
    year_of_study INTEGER,
    institution TEXT,
    city TEXT NOT NULL,
    country TEXT NOT NULL,
    student_id TEXT NOT NULL,
    national_id_hash TEXT NOT NULL UNIQUE,
    skills TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(skills) AND json_type(skills) = 'array'),
    interests TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(interests) AND json_type(interests) = 'array'),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (year_of_study IS NULL OR year_of_study > 0)
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    student_id INTEGER NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (student_id) REFERENCES students(id) ON DELETE CASCADE
);

CREATE TABLE companies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    industry TEXT NOT NULL,
    city TEXT NOT NULL,
    country TEXT NOT NULL,
    summary TEXT NOT NULL,
    programs TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(programs) AND json_type(programs) = 'array'),
    skills TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(skills) AND json_type(skills) = 'array'),
    remote INTEGER NOT NULL DEFAULT 0 CHECK (remote IN (0, 1)),
    active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE preferences (
    student_id INTEGER NOT NULL,
    company_id INTEGER NOT NULL,
    signal TEXT NOT NULL CHECK (signal IN ('like', 'dislike')),
    reason TEXT CHECK (reason IS NULL OR reason IN ('location', 'industry', 'program', 'other')),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (student_id, company_id),
    FOREIGN KEY (student_id) REFERENCES students(id) ON DELETE CASCADE,
    FOREIGN KEY (company_id) REFERENCES companies(id) ON DELETE CASCADE
);

CREATE INDEX idx_sessions_student_id ON sessions(student_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX idx_companies_active ON companies(active);
CREATE INDEX idx_preferences_company_id ON preferences(company_id);
CREATE INDEX idx_preferences_student_signal ON preferences(student_id, signal);
