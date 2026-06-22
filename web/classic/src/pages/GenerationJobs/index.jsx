import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Button,
  Card,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  TextArea,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { IconHelpCircle } from '@douyinfe/semi-icons';
import {
  API,
  getUserIdFromLocalStorage,
  isAdmin,
  showError,
  showSuccess,
  timestamp2string,
} from '../../helpers';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

const statusColors = {
  queued: 'blue',
  running: 'amber',
  succeeded: 'green',
  failed: 'red',
  cancelled: 'grey',
};

const GenerationJobs = () => {
  const { t } = useTranslation();
  const admin = isAdmin();
  const currentUserId = getUserIdFromLocalStorage();
  const [loading, setLoading] = useState(false);
  const [jobs, setJobs] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [status, setStatus] = useState('');
  const [path, setPath] = useState('');
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [cancellingJobId, setCancellingJobId] = useState('');
  const [retryingJobId, setRetryingJobId] = useState('');
  const [tokens, setTokens] = useState([]);
  const [generationJobTokenGroup, setGenerationJobTokenGroup] = useState();
  const [selectedTokenId, setSelectedTokenId] = useState();
  const [createForm, setCreateForm] = useState({
    model: 'gpt-image-2',
    prompt: '',
    size: '1024x1024',
    customSize: '',
    n: 1,
    referenceFiles: [],
  });

  const fetchJobs = async ({ silent = false } = {}) => {
    if (!silent) {
      setLoading(true);
    }
    try {
      const endpoint = admin
        ? '/api/generation_jobs/'
        : '/api/generation_jobs/self';
      const res = await API.get(endpoint, {
        params: {
          p: page,
          page_size: pageSize,
          status: status || undefined,
          path: path || undefined,
        },
      });
      setJobs(res.data?.items || []);
      setTotal(res.data?.total || 0);
    } catch (error) {
      showError(error);
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  };

  const fetchTokens = async () => {
    try {
      const res = await API.get('/api/token/', {
        params: { p: 1, size: 100, generation_job: true },
        disableDuplicate: true,
      });
      const data = res.data?.data || {};
      const items = data.items || [];
      setTokens(items);
      setGenerationJobTokenGroup(
        Object.prototype.hasOwnProperty.call(
          data,
          'generation_job_token_group',
        )
          ? data.generation_job_token_group
          : items[0]?.group,
      );
      if (!selectedTokenId && items.length > 0) {
        setSelectedTokenId(items[0].id);
      }
    } catch (error) {
      showError(error);
    }
  };

  useEffect(() => {
    fetchJobs();
  }, [admin, page, pageSize, status, path]);

  useEffect(() => {
    if (!jobs.some((job) => ['queued', 'running'].includes(job.status))) {
      return undefined;
    }
    const timer = setInterval(() => {
      fetchJobs({ silent: true });
    }, 5000);
    return () => clearInterval(timer);
  }, [jobs, admin, page, pageSize, status, path]);

  useEffect(() => {
    if (createModalOpen) {
      fetchTokens();
    }
  }, [createModalOpen]);

  const createGenerationJob = async () => {
    if (!selectedTokenId) {
      showError(t('请选择令牌'));
      return;
    }
    if (!createForm.prompt.trim()) {
      showError(t('请输入提示词'));
      return;
    }
    setCreating(true);
    try {
      const keyRes = await API.post(`/api/token/${selectedTokenId}/key`, null, {
        skipErrorHandler: true,
      });
      const tokenKey = keyRes.data?.data?.key;
      if (!tokenKey) {
        throw new Error(t('获取令牌 key 失败'));
      }

      const authHeader = `Bearer sk-${tokenKey}`;
      if (createForm.referenceFiles.length > 0) {
        const images = [];
        for (const file of createForm.referenceFiles) {
          const formData = new FormData();
          formData.append('image', file);
          const uploadRes = await API.post(
            '/api/generation_jobs/upload_image',
            formData,
            { skipErrorHandler: true },
          );
          const imageUrl = uploadRes.data?.url;
          if (!imageUrl) {
            throw new Error(t('上传参考图失败：{{name}}', { name: file.name }));
          }
          images.push({ image_url: imageUrl });
        }
        await API.post(
          '/v1/images/edits/jobs',
          {
            model: createForm.model,
            prompt: createForm.prompt.trim(),
            size: getCreateJobSize(createForm),
            n: createForm.n,
            images,
          },
          {
            headers: { Authorization: authHeader },
            skipErrorHandler: true,
          },
        );
      } else {
        await API.post(
          '/v1/images/generations/jobs',
          {
            model: createForm.model,
            prompt: createForm.prompt.trim(),
            size: getCreateJobSize(createForm),
            n: createForm.n,
          },
          { headers: { Authorization: authHeader }, skipErrorHandler: true },
        );
      }
      showSuccess(t('生图任务已创建'));
      setCreateModalOpen(false);
      setCreateForm((prev) => ({ ...prev, prompt: '', referenceFiles: [] }));
      setPage(1);
      fetchJobs();
    } catch (error) {
      showError(getCreateJobErrorMessage(error, t));
    } finally {
      setCreating(false);
    }
  };

  const cancelGenerationJob = (job) => {
    Modal.confirm({
      title: t('取消生图任务'),
      content:
        job.status === 'running'
          ? t('该任务已经开始运行，取消后仍会按已执行任务扣费。确认取消吗？')
          : t('该任务仍在队列中，取消后不会扣费。确认取消吗？'),
      okText: t('确认取消'),
      cancelText: t('再想想'),
      okButtonProps: { type: 'danger' },
      onOk: async () => {
        setCancellingJobId(job.job_id);
        try {
          await API.delete(`/api/generation_jobs/${job.job_id}`);
          showSuccess(t('生图任务已取消'));
          fetchJobs();
        } catch (error) {
          showError(error);
        } finally {
          setCancellingJobId('');
        }
      },
    });
  };

  const retryGenerationJob = (job) => {
    Modal.confirm({
      title: t('重试生图任务'),
      content: t(
        '将基于原始输入创建一条新的队列任务，并重新预扣额度。确认重试吗？',
      ),
      okText: t('确认重试'),
      cancelText: t('取消'),
      onOk: async () => {
        setRetryingJobId(job.job_id);
        try {
          await API.post(`/api/generation_jobs/${job.job_id}/retry`);
          showSuccess(t('已创建新的生图任务'));
          setPage(1);
          fetchJobs();
        } catch (error) {
          showError(error);
        } finally {
          setRetryingJobId('');
        }
      },
    });
  };

  const columns = useMemo(() => {
    const pathLabels = {
      '/v1/images/generations': t('文生图'),
      '/v1/images/edits': t('改图'),
    };
    const tableColumns = [
      {
        title: t('类型'),
        dataIndex: 'path',
        width: 90,
        render: (value) => pathLabels[value] || value || '-',
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        width: 110,
        render: (value, record) => renderJobStatus(value, record, t),
      },
      {
        title: t('模型'),
        dataIndex: 'model',
        width: 150,
      },
      {
        title: t('输入'),
        dataIndex: 'input',
        width: 220,
        render: (value) => <InputPreviewCell input={value} />,
      },
      {
        title: t('图片数'),
        dataIndex: 'image_count',
        width: 90,
        render: (value) => value || '-',
      },
      {
        title: t('创建时间'),
        dataIndex: 'created_at',
        width: 170,
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
      {
        title: t('完成时间'),
        dataIndex: 'finished_at',
        width: 170,
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
      {
        title: t('结果'),
        dataIndex: 'response_body',
        width: 220,
        render: (value) => renderResultLinks(value, t),
      },
      {
        title: t('错误'),
        dataIndex: 'fail_reason',
        width: 260,
        render: (value) =>
          value ? (
            <div className='max-w-[240px] whitespace-pre-wrap break-words text-xs leading-tight text-red-500'>
              {value}
            </div>
          ) : (
            '-'
          ),
      },
      {
        title: t('操作'),
        dataIndex: 'operation',
        width: 100,
        fixed: 'right',
        render: (_, record) => {
          const isOwnJob = Number(record.user_id) === Number(currentUserId);
          if (!isOwnJob) {
            return '-';
          }
          if (['succeeded', 'failed', 'cancelled'].includes(record.status)) {
            return (
              <Button
                type='primary'
                size='small'
                theme='borderless'
                loading={retryingJobId === record.job_id}
                onClick={() => retryGenerationJob(record)}
              >
                {t('重试')}
              </Button>
            );
          }
          if (!['queued', 'running'].includes(record.status)) {
            return '-';
          }
          return (
            <Button
              type='danger'
              size='small'
              theme='borderless'
              loading={cancellingJobId === record.job_id}
              onClick={() => cancelGenerationJob(record)}
            >
              {t('取消')}
            </Button>
          );
        },
      },
    ];

    if (admin) {
      tableColumns.unshift(
        {
          title: t('任务 ID'),
          dataIndex: 'job_id',
          width: 110,
          render: (text) => (
            <Text copyable={{ content: text }}>{formatJobId(text)}</Text>
          ),
        },
        {
          title: t('用户'),
          dataIndex: 'user_id',
          width: 90,
        },
      );
    }

    return tableColumns;
  }, [admin, cancellingJobId, currentUserId, retryingJobId, t]);

  return (
    <div className='mt-[60px] px-2'>
      <Card className='table-scroll-card !rounded-2xl'>
        <div className='flex flex-col gap-3'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <div>
              <div className='text-lg font-semibold'>{t('生图任务')}</div>
              <div className='text-sm text-gray-500'>
                {t(
                  '查看异步文生图和改图任务状态，生成的图片请尽快下载，过期后将删除。',
                )}
              </div>
            </div>
            <Space wrap>
              <Button type='primary' onClick={() => setCreateModalOpen(true)}>
                {t('创建生图任务')}
              </Button>
              <Select
                value={path}
                onChange={(value) => {
                  setPath(value);
                  setPage(1);
                }}
                style={{ width: 140 }}
              >
                <Select.Option value=''>{t('全部类型')}</Select.Option>
                <Select.Option value='/v1/images/generations'>
                  {t('文生图')}
                </Select.Option>
                <Select.Option value='/v1/images/edits'>
                  {t('改图')}
                </Select.Option>
              </Select>
              <Select
                value={status}
                onChange={(value) => {
                  setStatus(value);
                  setPage(1);
                }}
                style={{ width: 140 }}
              >
                <Select.Option value=''>{t('全部状态')}</Select.Option>
                <Select.Option value='queued'>queued</Select.Option>
                <Select.Option value='running'>running</Select.Option>
                <Select.Option value='succeeded'>succeeded</Select.Option>
                <Select.Option value='failed'>failed</Select.Option>
                <Select.Option value='cancelled'>cancelled</Select.Option>
              </Select>
              <Button onClick={fetchJobs} loading={loading}>
                {t('刷新')}
              </Button>
            </Space>
          </div>
          <Table
            className='generation-jobs-table'
            columns={columns}
            dataSource={jobs}
            rowKey='job_id'
            loading={loading}
            scroll={{ x: 'max-content' }}
            pagination={{
              currentPage: page,
              pageSize,
              total,
              showSizeChanger: true,
              pageSizeOptions: [10, 20, 50, 100],
              onPageChange: setPage,
              onPageSizeChange: (size) => {
                setPageSize(size);
                setPage(1);
              },
            }}
          />
        </div>
      </Card>
      <CreateGenerationJobModal
        visible={createModalOpen}
        creating={creating}
        tokens={tokens}
        generationJobTokenGroup={generationJobTokenGroup}
        selectedTokenId={selectedTokenId}
        setSelectedTokenId={setSelectedTokenId}
        form={createForm}
        setForm={setCreateForm}
        onCancel={() => setCreateModalOpen(false)}
        onOk={createGenerationJob}
      />
    </div>
  );
};

const sizeOptions = [
  { label: '1:1', value: '1024x1024', icon: 'aspect-square' },
  { label: '4:3', value: '1024x768', icon: 'aspect-[4/3]' },
  { label: '16:9', value: '1024x576', icon: 'aspect-video' },
  { label: '9:16', value: '576x1024', icon: 'aspect-[9/16]' },
  { label: '3:2', value: '1024x682', icon: 'aspect-[3/2]' },
  { label: 'Auto', value: 'auto', icon: 'aspect-square' },
  { label: '自定义', value: 'custom', icon: 'aspect-square' },
];

function getCreateJobSize(form) {
  if (form.size === 'custom') {
    return form.customSize.trim() || 'auto';
  }
  return form.size;
}

function formatJobId(jobId) {
  if (!jobId) {
    return '-';
  }
  return `...${String(jobId).slice(-8)}`;
}

function renderJobStatus(status, job) {
  if (isPartiallySucceededJob(status, job)) {
    return <Tag color='amber'>部分成功</Tag>;
  }
  return <Tag color={statusColors[status] || 'grey'}>{status}</Tag>;
}

function isPartiallySucceededJob(status, job) {
  if (status !== 'succeeded' || !job?.image_count || job.image_count <= 1) {
    return false;
  }
  const generatedImageCount = getGeneratedImageCount(job.response_body);
  return generatedImageCount > 0 && generatedImageCount < job.image_count;
}

function getGenerationJobTokenGroupTip(group, t) {
  if (group === undefined) {
    return t('正在读取生图任务令牌分组配置...');
  }

  if (!group) {
    return t('当前未配置生图任务令牌分组，这里会显示你的全部令牌。');
  }

  return t('请先创建分组为 {{group}} 的令牌，否则这里不会显示可用令牌。', {
    group,
  });
}

function CreateGenerationJobModal({
  visible,
  creating,
  tokens,
  generationJobTokenGroup,
  selectedTokenId,
  setSelectedTokenId,
  form,
  setForm,
  onCancel,
  onOk,
}) {
  const { t } = useTranslation();
  const fileInputRef = useRef(null);
  const [isDragging, setIsDragging] = useState(false);
  const [previewUrls, setPreviewUrls] = useState([]);

  useEffect(() => {
    if (!form.referenceFiles.length) {
      setPreviewUrls([]);
      return undefined;
    }
    const nextUrls = form.referenceFiles.map((file) =>
      URL.createObjectURL(file),
    );
    setPreviewUrls(nextUrls);
    return () => nextUrls.forEach((url) => URL.revokeObjectURL(url));
  }, [form.referenceFiles]);

  const addReferenceFiles = (fileList) => {
    const files = Array.from(fileList || []);
    const invalidFile = files.find((file) => !file.type.startsWith('image/'));
    if (invalidFile) {
      showError(t('请上传图片文件'));
      return;
    }
    if (files.length === 0) {
      return;
    }
    setForm((prev) => ({
      ...prev,
      referenceFiles: [...prev.referenceFiles, ...files],
    }));
  };

  const removeReferenceFile = (index) => {
    setForm((prev) => ({
      ...prev,
      referenceFiles: prev.referenceFiles.filter(
        (_, fileIndex) => fileIndex !== index,
      ),
    }));
  };

  const handleDrop = (event) => {
    event.preventDefault();
    setIsDragging(false);
    addReferenceFiles(event.dataTransfer.files);
  };

  return (
    <Modal
      title={t('创建生图任务')}
      visible={visible}
      onCancel={onCancel}
      onOk={onOk}
      confirmLoading={creating}
      okText={t('创建')}
      cancelText={t('取消')}
      width={620}
    >
      <div className='flex flex-col gap-4'>
        <div>
          <div className='mb-1 flex items-center gap-1 text-sm font-medium'>
            <span>{t('使用令牌')}</span>
            <Tooltip
              content={getGenerationJobTokenGroupTip(generationJobTokenGroup, t)}
              position='top'
              showArrow
            >
              <IconHelpCircle className='cursor-help text-gray-400' />
            </Tooltip>
          </div>
          <Select
            value={selectedTokenId}
            onChange={setSelectedTokenId}
            style={{ width: '100%' }}
            placeholder={t('请选择令牌')}
          >
            {tokens.map((token) => (
              <Select.Option key={token.id} value={token.id}>
                <div className='flex items-center gap-2 min-w-0'>
                  <Tag color='blue' size='small'>
                    {token.group || 'default'}
                  </Tag>
                  <span className='font-medium truncate'>
                    {token.name || `Token ${token.id}`}
                  </span>
                  <span className='text-xs text-gray-500 truncate'>
                    sk-{token.key}
                  </span>
                </div>
              </Select.Option>
            ))}
          </Select>
        </div>
        <div>
          <div className='mb-1 text-sm font-medium'>{t('模型')}</div>
          <div className='rounded-lg border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-1)] px-3 py-2 text-sm font-medium text-[var(--semi-color-text-0)]'>
            gpt-image-2
          </div>
        </div>
        <div className='grid grid-cols-1 gap-3 md:grid-cols-2'>
          <div>
            <div className='mb-1 text-sm font-medium'>{t('生成张数')}</div>
            <InputNumber
              value={form.n}
              min={1}
              max={10}
              onChange={(value) =>
                setForm((prev) => ({ ...prev, n: Number(value) || 1 }))
              }
              style={{ width: '100%' }}
            />
          </div>
          <div>
            <div className='mb-1 text-sm font-medium'>{t('当前尺寸')}</div>
            <Input
              value={getCreateJobSize(form)}
              onChange={(value) =>
                setForm((prev) => ({
                  ...prev,
                  size: 'custom',
                  customSize: value,
                }))
              }
              placeholder={t('例如：1024x1024 或 auto')}
            />
          </div>
        </div>
        <div>
          <div className='mb-2 text-sm font-medium'>{t('图片比例 / 尺寸')}</div>
          <div className='grid grid-cols-3 gap-2 md:grid-cols-7'>
            {sizeOptions.map((option) => {
              const active = form.size === option.value;
              return (
                <button
                  key={option.value}
                  type='button'
                  className='relative flex flex-col items-center justify-center gap-1 rounded-lg border px-2 py-2 text-xs transition-colors hover:border-[var(--semi-color-primary)] hover:text-[var(--semi-color-primary)]'
                  style={{
                    borderColor: active
                      ? 'var(--semi-color-primary)'
                      : 'var(--semi-color-border)',
                    background: active
                      ? 'var(--semi-color-primary)'
                      : 'var(--semi-color-bg-1)',
                    color: active
                      ? 'var(--semi-color-white)'
                      : 'var(--semi-color-text-2)',
                    boxShadow: active
                      ? '0 0 0 2px var(--semi-color-primary-light-default)'
                      : 'none',
                  }}
                  onClick={() =>
                    setForm((prev) => ({ ...prev, size: option.value }))
                  }
                >
                  {active && (
                    <span className='absolute right-1 top-1 flex h-4 w-4 items-center justify-center rounded-full bg-[var(--semi-color-primary)] text-[10px] text-white'>
                      ✓
                    </span>
                  )}
                  <span
                    className={`block w-7 rounded border-2 ${option.icon}`}
                    style={{
                      borderColor: active
                        ? 'var(--semi-color-white)'
                        : 'var(--semi-color-text-2)',
                      background: active
                        ? 'rgba(255, 255, 255, 0.18)'
                        : 'transparent',
                    }}
                  />
                  <span>{t(option.label)}</span>
                </button>
              );
            })}
          </div>
        </div>
        <div>
          <div className='mb-1 text-sm font-medium'>{t('提示词')}</div>
          <TextArea
            value={form.prompt}
            rows={4}
            onChange={(value) =>
              setForm((prev) => ({ ...prev, prompt: value }))
            }
            placeholder={t('描述你想生成或修改的图片')}
          />
        </div>
        <div>
          <div className='mb-1 text-sm font-medium'>{t('参考图（可选）')}</div>
          <input
            ref={fileInputRef}
            type='file'
            accept='image/*'
            multiple
            className='hidden'
            onChange={(event) => {
              addReferenceFiles(event.target.files);
              event.target.value = '';
            }}
          />
          <div
            role='button'
            tabIndex={0}
            onClick={() => fileInputRef.current?.click()}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                fileInputRef.current?.click();
              }
            }}
            onDragOver={(event) => {
              event.preventDefault();
              setIsDragging(true);
            }}
            onDragLeave={() => setIsDragging(false)}
            onDrop={handleDrop}
            className={`flex min-h-[120px] cursor-pointer flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed p-4 transition-colors ${
              isDragging
                ? 'border-blue-500 bg-blue-500/10'
                : 'border-gray-300 bg-gray-50 hover:border-blue-400 hover:bg-blue-50/50'
            }`}
          >
            {previewUrls.length > 0 ? (
              <div className='grid w-full grid-cols-2 gap-3 sm:grid-cols-3'>
                {previewUrls.map((url, index) => (
                  <div
                    key={`${url}-${index}`}
                    className='relative overflow-hidden rounded-lg border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-1)]'
                  >
                    <img
                      src={url}
                      alt={t('参考图 {{index}}', { index: index + 1 })}
                      className='h-24 w-full object-cover'
                    />
                    <button
                      type='button'
                      className='absolute right-1 top-1 rounded bg-black/60 px-2 py-0.5 text-xs text-white'
                      onClick={(event) => {
                        event.stopPropagation();
                        removeReferenceFile(index);
                      }}
                    >
                      {t('移除')}
                    </button>
                    <div className='truncate px-2 py-1 text-xs text-[var(--semi-color-text-2)]'>
                      {form.referenceFiles[index]?.name}
                    </div>
                  </div>
                ))}
                <div className='flex h-[122px] items-center justify-center rounded-lg border border-dashed border-[var(--semi-color-border)] text-xs text-[var(--semi-color-text-2)]'>
                  {t('点击或拖拽继续添加')}
                </div>
              </div>
            ) : (
              <>
                <div className='flex h-11 w-11 items-center justify-center rounded-full bg-blue-500/10 text-blue-500'>
                  ↑
                </div>
                <div className='text-sm font-medium'>
                  {t('点击或拖拽图片到这里')}
                </div>
                <div className='text-xs text-gray-500'>
                  {t('支持多张 JPG、PNG、WebP')}
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </Modal>
  );
}

function getCreateJobErrorMessage(error, t) {
  const data = error?.response?.data;
  return (
    data?.error?.message ||
    data?.message ||
    error?.message ||
    t('创建生图任务失败')
  );
}

function InputPreviewCell({ input }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const prompt = input?.prompt?.trim?.() || '';
  const size = input?.size || input?.image_size || input?.dimensions || '';
  const imageUrls = Array.isArray(input?.reference_image_urls)
    ? input.reference_image_urls.filter(Boolean)
    : [];
  if (!prompt && !size && imageUrls.length === 0) {
    return '-';
  }
  const summary =
    prompt || t('{{count}} 张参考图', { count: imageUrls.length });
  const metaItems = [
    size ? t('尺寸 {{size}}', { size }) : '',
    imageUrls.length > 0
      ? t('{{count}} 张参考图', { count: imageUrls.length })
      : '',
  ].filter(Boolean);
  return (
    <>
      <button
        type='button'
        className='max-w-[200px] cursor-pointer text-left hover:text-semi-color-primary'
        onClick={() => setOpen(true)}
      >
        <div className='line-clamp-3 whitespace-pre-wrap break-words text-xs leading-tight text-semi-color-text-0'>
          {summary}
        </div>
        {metaItems.length > 0 && (
          <div className='mt-0.5 text-[11px] leading-tight text-[var(--semi-color-text-2)]'>
            {metaItems.join(' · ')}
          </div>
        )}
      </button>
      <Modal
        title={t('输入详情')}
        visible={open}
        footer={null}
        onCancel={() => setOpen(false)}
        width={640}
      >
        <InputTooltipContent
          prompt={prompt}
          size={size}
          imageUrls={imageUrls}
        />
      </Modal>
    </>
  );
}

function InputTooltipContent({ prompt, size, imageUrls }) {
  const { t } = useTranslation();
  return (
    <div className='max-w-[420px] space-y-3 break-words pb-4 text-sm'>
      <div>
        <div className='mb-1 font-medium'>{t('提示词')}</div>
        <div className='whitespace-pre-wrap'>{prompt || '-'}</div>
      </div>
      <div>
        <div className='mb-1 font-medium'>{t('图片尺寸')}</div>
        <div>{size || '-'}</div>
      </div>
      <div>
        <div className='mb-1 font-medium'>{t('参考图')}</div>
        {imageUrls.length > 0 ? (
          <div className='space-y-1'>
            {imageUrls.map((url, index) => (
              <a
                key={`${url}-${index}`}
                href={url}
                target='_blank'
                rel='noreferrer'
                className='block max-w-[400px] truncate text-blue-500 hover:underline'
              >
                {url}
              </a>
            ))}
          </div>
        ) : (
          '-'
        )}
      </div>
    </div>
  );
}

function renderResultLinks(responseBody, t) {
  const body = normalizeResponseBody(responseBody);
  const urls = getGeneratedImageUrls(body);
  if (urls.length === 0) {
    return '-';
  }
  return (
    <Space vertical align='start' spacing={4}>
      {urls.map((url, index) => (
        <Text
          key={`${url}-${index}`}
          copyable={{ content: url }}
          style={{ display: 'block' }}
        >
          <a
            href={url}
            target='_blank'
            rel='noreferrer'
            title={url}
            className='inline-flex rounded-full border border-semi-color-border px-2.5 py-1 text-xs leading-none text-semi-color-primary hover:border-semi-color-primary hover:bg-semi-color-primary-light-default'
          >
            {t('图片 {{index}}', { index: index + 1 })}
          </a>
        </Text>
      ))}
    </Space>
  );
}

function getGeneratedImageCount(responseBody) {
  const body = normalizeResponseBody(responseBody);
  if (!Array.isArray(body?.data)) {
    return 0;
  }
  return body.data.filter((item) => item?.url || item?.b64_json).length;
}

function getGeneratedImageUrls(body) {
  return Array.isArray(body?.data)
    ? body.data.map((item) => item?.url).filter(Boolean)
    : [];
}

function normalizeResponseBody(responseBody) {
  if (typeof responseBody !== 'string') {
    return responseBody;
  }
  try {
    return JSON.parse(responseBody);
  } catch {
    return null;
  }
}

export default GenerationJobs;
