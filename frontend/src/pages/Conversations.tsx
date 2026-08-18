import { useEffect } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { MessagesSquare, Send, Trash2 } from 'lucide-react';
import { useConversationStore } from '@/stores/conversationStore';
import { useT } from '@/i18n';
import { Button } from '@/components/ui';

export default function Conversations() {
  const { conversationId } = useParams<{ conversationId: string }>();
  const navigate = useNavigate();
  const t = useT();
  const conversations = useConversationStore((s) => s.conversations);
  const current = useConversationStore((s) => s.current);
  const messages = useConversationStore((s) => s.messages);
  const loading = useConversationStore((s) => s.loading);
  const sending = useConversationStore((s) => s.sending);
  const error = useConversationStore((s) => s.error);

  useEffect(() => {
    void useConversationStore.getState().fetchConversations();
    if (conversationId) {
      void useConversationStore.getState().openConversation(conversationId);
    } else {
      useConversationStore.getState().resetConversation();
    }
  }, [conversationId]);

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = event.currentTarget;
    const input = form.elements.namedItem('message') as HTMLTextAreaElement;
    const message = input.value.trim();
    if (!message) return;
    try {
      const id = await useConversationStore.getState().ask(message, conversationId);
      input.value = '';
      if (!conversationId) navigate(`/conversations/${encodeURIComponent(id)}`);
    } catch {
      // The store exposes the API error below the form.
    }
  };

  const handleDelete = async () => {
    if (!current) return;
    if (!window.confirm(t('conversations.deleteConfirm'))) return;
    await useConversationStore.getState().deleteConversation(current.id);
    navigate('/conversations');
  };

  return (
    <div className="grid h-full grid-cols-1 gap-4 overflow-hidden p-4 lg:grid-cols-[280px_1fr]">
      <aside className="flex min-h-0 flex-col rounded-lg border border-border-dim bg-elevated">
        <div className="flex items-center gap-2 border-b border-border-dim px-4 py-3">
          <MessagesSquare size={16} className="text-accent" />
          <h2 className="font-mono text-xs font-semibold uppercase tracking-wider text-text-secondary">
            {t('conversations.title')}
          </h2>
        </div>
        <div className="flex-1 overflow-y-auto p-2">
          {loading && conversations.length === 0 && (
            <p className="px-2 py-3 font-mono text-xs text-text-muted">{t('conversations.loading')}</p>
          )}
          {!loading && conversations.length === 0 && (
            <p className="px-2 py-3 font-mono text-xs text-text-muted">{t('conversations.empty')}</p>
          )}
          {conversations.map((conversation) => (
            <Link
              key={conversation.id}
              to={`/conversations/${encodeURIComponent(conversation.id)}`}
              className={`block rounded-md px-3 py-2 transition-colors ${
                conversation.id === conversationId
                  ? 'bg-accent/10 text-accent'
                  : 'text-text-secondary hover:bg-raised hover:text-text-primary'
              }`}
            >
              <span className="line-clamp-2 block font-mono text-xs">{conversation.title}</span>
              <span className="mt-1 block font-mono text-[10px] text-text-muted">
                {new Date(conversation.updatedAt).toLocaleString()}
              </span>
            </Link>
          ))}
        </div>
      </aside>

      <section className="flex min-h-0 flex-col rounded-lg border border-border-dim bg-elevated">
        <div className="flex items-center justify-between border-b border-border-dim px-4 py-3">
          <div className="min-w-0">
            <h1 className="truncate font-mono text-sm font-semibold text-text-primary">
              {current?.title || t('conversations.new')}
            </h1>
            {current && (
              <p className="font-mono text-[10px] text-text-muted">{current.id}</p>
            )}
          </div>
          {current && (
            <Button type="button" variant="ghost" size="sm" onClick={() => void handleDelete()}>
              <Trash2 size={12} /> {t('conversations.delete')}
            </Button>
          )}
        </div>

        <div className="flex-1 space-y-3 overflow-y-auto p-4">
          {messages.length === 0 && (
            <p className="font-mono text-xs text-text-muted">{t('conversations.emptyThread')}</p>
          )}
          {messages.map((message) => (
            <article
              key={message.id}
              className={`max-w-3xl rounded-lg border px-3 py-2 ${
                message.role === 'user'
                  ? 'ml-auto border-accent/30 bg-accent/10'
                  : 'border-border-dim bg-raised'
              }`}
            >
              <p className="whitespace-pre-wrap font-mono text-xs text-text-secondary">{message.content}</p>
              {message.caseId && (
                <Link
                  to={`/case/${encodeURIComponent(message.caseId)}`}
                  className="mt-2 inline-block font-mono text-[10px] text-accent hover:underline"
                >
                  {t('conversations.openCase')}
                </Link>
              )}
            </article>
          ))}
        </div>

        <form onSubmit={handleSubmit} className="border-t border-border-dim p-4">
          <textarea
            name="message"
            rows={3}
            required
            disabled={sending}
            placeholder={conversationId ? t('conversations.followUpPlaceholder') : t('conversations.newPlaceholder')}
            className="w-full resize-y rounded-md border border-border-dim bg-base px-3 py-2 font-mono text-xs text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          <div className="mt-3 flex items-center justify-between gap-3">
            <p className="font-mono text-[10px] text-text-muted">
              {conversationId ? t('conversations.contextHint') : t('conversations.autoRunHint')}
            </p>
            <Button type="submit" variant="accent" disabled={sending}>
              <Send size={12} /> {sending ? t('conversations.sending') : t('conversations.send')}
            </Button>
          </div>
          {error && <p className="mt-2 font-mono text-xs text-error">{error}</p>}
        </form>
      </section>
    </div>
  );
}
