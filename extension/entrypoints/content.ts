export default defineContentScript({
  matches: ['*://*/*'],
  runAt: 'document_start',
  main() {
    console.log('[SheepGet] Content script injected');
  },
});
