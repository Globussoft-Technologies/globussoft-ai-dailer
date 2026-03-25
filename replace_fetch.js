const fs = require('fs');
const filepath = 'c:/Users/Admin/gbs-projects/gbs-ai-dialer/frontend/src/App.jsx';
let content = fs.readFileSync(filepath, 'utf8');

const fetchRegex = /\bfetch\((.*?)\)/g;

let updatedContent = content.replace(fetchRegex, (match, p1) => {
    if (p1.includes('/auth/login') || p1.includes('/auth/signup') || p1.includes('/auth/me')) {
        return match;
    }
    return `apiFetch(${p1})`;
});

fs.writeFileSync(filepath, updatedContent, 'utf8');
console.log('Successfully replaced fetch with apiFetch in App.jsx');
