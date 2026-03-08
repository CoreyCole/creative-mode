import { Type } from '@mariozechner/pi-ai';
import { readFileSync, readdirSync } from 'fs';
import { join, relative } from 'path';
import { execSync } from 'child_process';

const STOPWORDS = new Set([
  'the', 'a', 'an', 'is', 'are', 'was', 'were', 'be', 'been', 'being',
  'have', 'has', 'had', 'do', 'does', 'did', 'will', 'would', 'could',
  'should', 'may', 'might', 'shall', 'can', 'need', 'dare', 'ought',
  'used', 'to', 'of', 'in', 'for', 'on', 'with', 'at', 'by', 'from',
  'as', 'into', 'through', 'during', 'before', 'after', 'above', 'below',
  'between', 'out', 'off', 'over', 'under', 'again', 'further', 'then',
  'once', 'here', 'there', 'when', 'where', 'why', 'how', 'all', 'each',
  'every', 'both', 'few', 'more', 'most', 'other', 'some', 'such', 'no',
  'nor', 'not', 'only', 'own', 'same', 'so', 'than', 'too', 'very',
  'just', 'because', 'but', 'and', 'or', 'if', 'while', 'about', 'what',
  'which', 'who', 'whom', 'this', 'that', 'these', 'those', 'i', 'me',
  'my', 'we', 'our', 'you', 'your', 'he', 'him', 'his', 'she', 'her',
  'it', 'its', 'they', 'them', 'their'
]);

export function extractKeywords(query) {
  return query
    .toLowerCase()
    .split(/[\s,;:.()[\]{}'"]+/)
    .filter(w => w.length > 2 && !STOPWORDS.has(w));
}

let skillIndexCache = null;

function parseFrontmatter(content) {
  const match = content.match(/^---\n([\s\S]*?)\n---/);
  if (!match) return {};
  const fm = {};
  for (const line of match[1].split('\n')) {
    const colonIdx = line.indexOf(':');
    if (colonIdx === -1) continue;
    const key = line.slice(0, colonIdx).trim();
    let val = line.slice(colonIdx + 1).trim();
    if (val.startsWith('[') && val.endsWith(']')) {
      val = val.slice(1, -1).split(',').map(s => s.trim());
    }
    fm[key] = val;
  }
  return fm;
}

export function loadSkillIndex(skillsDir) {
  if (skillIndexCache) return skillIndexCache;
  const index = [];
  try {
    const files = readdirSync(skillsDir).filter(f => f.endsWith('.md'));
    for (const file of files) {
      const fullPath = join(skillsDir, file);
      const content = readFileSync(fullPath, 'utf-8');
      const fm = parseFrontmatter(content);
      index.push({
        path: fullPath,
        file,
        name: fm.name || file.replace('.md', ''),
        description: fm.description || '',
        tags: Array.isArray(fm.tags) ? fm.tags : [],
      });
    }
  } catch (_e) {
    // Skills dir may not exist yet
  }
  skillIndexCache = index;
  return index;
}

export function rankByRelevance(skillIndex, keywords) {
  return skillIndex
    .map(skill => {
      const searchText = `${skill.description} ${skill.tags.join(' ')}`.toLowerCase();
      const score = keywords.reduce((acc, kw) => acc + (searchText.includes(kw) ? 1 : 0), 0);
      return { ...skill, score };
    })
    .filter(s => s.score > 0)
    .sort((a, b) => b.score - a.score);
}

function grepFiles(searchDirs, keywords, opts = {}) {
  const { glob = '*.{go,templ,sql,js,md}', maxResults = 10 } = opts;
  const results = [];

  for (const dir of searchDirs) {
    for (const kw of keywords.slice(0, 3)) {
      try {
        const out = execSync(
          `grep -rl --include="${glob}" "${kw}" "${dir}" 2>/dev/null | head -${maxResults}`,
          { encoding: 'utf-8', timeout: 5000 }
        ).trim();
        if (!out) continue;
        for (const filePath of out.split('\n')) {
          if (!filePath || results.length >= maxResults) break;
          const relPath = relative(dir, filePath);
          if (!results.some(r => r.path === relPath)) {
            results.push({ path: relPath, keyword: kw });
          }
        }
      } catch (_e) {
        // grep returns non-zero when no matches
      }
      if (results.length >= maxResults) break;
    }
    if (results.length >= maxResults) break;
  }
  return results;
}

function formatResults(matchedSkills, codeMatches) {
  const parts = [];

  if (matchedSkills.length > 0) {
    parts.push('## Matching Skills\n');
    for (const skill of matchedSkills) {
      parts.push(`- **${skill.name}** (${skill.file}): ${skill.description} [score: ${skill.score}]`);
    }
  }

  if (codeMatches.length > 0) {
    parts.push('\n## Matching Source Files\n');
    for (const match of codeMatches) {
      parts.push(`- \`${match.path}\` (matched: "${match.keyword}")`);
    }
  }

  if (parts.length === 0) {
    return 'No matching skills or source files found for your query. Try different keywords or use file tools (grep, find) to search directly.';
  }

  parts.push('\nUse the `read` tool to load the full content of any relevant matches.');
  return parts.join('\n');
}

export function createSearchContextTool(skillsDir, searchDirs) {
  return {
    name: 'search_context',
    label: 'Search Context',
    description: 'Search for relevant skills, conventions, and code patterns in the codebase. Use this before starting work to discover what you need to know. Returns file paths and descriptions — use read to load full content.',
    parameters: Type.Object({
      query: Type.String({ description: 'Natural language description of what you need to know (e.g., "database migration patterns, SQLC conventions")' }),
      focus: Type.Optional(Type.Union([
        Type.Literal('all'),
        Type.Literal('skills-only'),
        Type.Literal('code-only')
      ], { description: 'Search focus: all (default), skills-only, or code-only', default: 'all' }))
    }),
    execute: async (_toolCallId, { query, focus = 'all' }) => {
      const keywords = extractKeywords(query);

      let matchedSkills = [];
      if (focus !== 'code-only') {
        const skillIndex = loadSkillIndex(skillsDir);
        matchedSkills = rankByRelevance(skillIndex, keywords);
      }

      let codeMatches = [];
      if (focus !== 'skills-only' && searchDirs.length > 0) {
        codeMatches = grepFiles(searchDirs, keywords);
      }

      const text = formatResults(matchedSkills, codeMatches);
      return {
        content: [{ type: 'text', text }],
        details: { skillCount: matchedSkills.length, codeCount: codeMatches.length }
      };
    }
  };
}
