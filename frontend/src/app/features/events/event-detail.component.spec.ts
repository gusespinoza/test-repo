import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap, provideRouter } from '@angular/router';
import { of } from 'rxjs';

import { EventDetail } from './events.models';
import { EventDetailComponent } from './event-detail.component';
import { EventsService } from './events.service';

describe('EventDetailComponent availability', () => {
  let fixture: ComponentFixture<EventDetailComponent>;
  let available: number;

  beforeEach(async () => {
    available = 0;
    await TestBed.configureTestingModule({
      imports: [EventDetailComponent],
      providers: [
        provideRouter([]),
        {
          provide: ActivatedRoute,
          useValue: { paramMap: of(convertToParamMap({ id: '00000000-0000-0000-0000-000000000001' })) }
        },
        {
          provide: EventsService,
          useValue: {
            getEvent: () =>
              of<EventDetail>({
                id: '00000000-0000-0000-0000-000000000001',
                title: 'Evento de prueba',
                description: '',
                venue: 'Auditorio',
                starts_at: '2026-12-01T10:00:00Z',
                capacity: 20,
                created_at: '2026-01-01T00:00:00Z',
                updated_at: '2026-01-01T00:00:00Z',
                available
              })
          }
        }
      ]
    }).compileComponents();
  });

  function render(): HTMLElement {
    fixture = TestBed.createComponent(EventDetailComponent);
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('shows sold out and disables purchase when availability is zero', () => {
    const host = render();

    expect(host.querySelector('.availability')?.textContent?.trim()).toBe('Agotado');
    expect((host.querySelector('.purchase-button') as HTMLButtonElement).disabled).toBeTrue();
  });

  it('shows the remaining ticket count below ten', () => {
    available = 4;
    const host = render();

    expect(host.querySelector('.availability')?.textContent?.trim()).toBe('Últimas 4 entradas');
    expect((host.querySelector('.purchase-button') as HTMLButtonElement).disabled).toBeFalse();
  });
});
